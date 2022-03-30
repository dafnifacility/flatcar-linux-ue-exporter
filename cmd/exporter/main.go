package main

import (
	"errors"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/dafnifacility/flatcar-linux-ue-exporter/internal/html"
	"github.com/flatcar-linux/flatcar-linux-update-operator/pkg/updateengine"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	log "github.com/sirupsen/logrus"
	"github.com/urfave/cli/v2"
)

var (
	metricLastCheckedTimestampSeconds = promauto.NewGauge(prometheus.GaugeOpts{
		Namespace: "coreos",
		Subsystem: "update_engine",
		Name:      "last_checked_time_s",
	})
	metricLastDBUSUpdate = promauto.NewGauge(prometheus.GaugeOpts{
		Namespace: "coreos",
		Subsystem: "update_engine",
		Name:      "last_dbus_update",
	})
	metricsHostStatus = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Namespace: "coreos",
		Subsystem: "update_engine",
		Name:      "status",
	}, []string{"op"})
	metricsCurrentStatusTime = promauto.NewGauge(prometheus.GaugeOpts{
		Namespace: "coreos",
		Subsystem: "update_engine",
		Name:      "time_in_current_status_s",
	})
)

func logRequestHandler(inner http.Handler) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		log.WithFields(log.Fields{
			"client":     r.RemoteAddr,
			"user-agent": r.Header.Get("user-agent"),
		}).Info(strings.Join([]string{r.Proto, r.Method, r.URL.Path}, " "))
		inner.ServeHTTP(w, r)
	}
}

func runWebServer(cc *cli.Context) {
	laddr := cc.String("listen-address")
	http.HandleFunc("/", logRequestHandler(http.FileServer(http.FS(html.Content))))
	http.HandleFunc("/metrics", logRequestHandler(promhttp.Handler()))
	log.WithField("listen-addr", laddr).Info("starting HTTP server for metrics")
	http.ListenAndServe(laddr, nil)
}

var currentStatus string
var lastStateChangeTimestampSeconds time.Time

const updateStatusPrefix = "UPDATE_STATUS_"

func updateOpstatus(newstate string) {
	for _, astate := range []string{
		updateengine.UpdateStatusIdle,
		updateengine.UpdateStatusCheckingForUpdate,
		updateengine.UpdateStatusUpdateAvailable,
		updateengine.UpdateStatusDownloading,
		updateengine.UpdateStatusVerifying,
		updateengine.UpdateStatusFinalizing,
		updateengine.UpdateStatusUpdatedNeedReboot,
		updateengine.UpdateStatusReportingErrorEvent,
	} {
		// Set the current state gauge to value 1, otherwise 0
		if astate == newstate {
			metricsHostStatus.WithLabelValues(astate[len(updateStatusPrefix):]).Set(1)
		} else {
			metricsHostStatus.WithLabelValues(astate[len(updateStatusPrefix):]).Set(0)
		}
	}
	if currentStatus != newstate {
		// Status has changed, set the variable
		lastStateChangeTimestampSeconds = time.Now()
		currentStatus = newstate
	}
	bumpTimeSinceUpdateGauge()
}

func bumpTimeSinceUpdateGauge() {
	metricsCurrentStatusTime.Set(time.Since(lastStateChangeTimestampSeconds).Truncate(time.Second).Seconds())
}

func runUpdateTime() {
	updater := time.Tick(60 * time.Second)
	for range updater {
		bumpTimeSinceUpdateGauge()
	}
}

func runExporter(cc *cli.Context) error {
	go runWebServer(cc)
	if !cc.Bool("pretend") {
		ue, err := updateengine.New()
		if err != nil {
			return err
		}
		us := make(chan updateengine.Status, 1)
		go ue.ReceiveStatuses(us, cc.Context.Done())
		log.Debug("started update engine status client")
		go runUpdateTime()
		for st := range us {
			log.WithField("status", st).Debug("engine status update")
			metricLastDBUSUpdate.SetToCurrentTime()
			metricLastCheckedTimestampSeconds.Set(float64(st.LastCheckedTime))
			updateOpstatus(st.CurrentOperation)
		}
		return nil
	} else {
		for {
			log.Warn("running in pretend mode - not actually checking with update engine!")
			updateOpstatus(updateengine.UpdateStatusIdle)
			runUpdateTime()
			time.Sleep(10 * time.Second)
		}
	}
}

func main() {
	app := &cli.App{
		Name:   "update-engine-exporter",
		Action: runExporter,
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:    "listen-address",
				Aliases: []string{"l"},
				Value:   ":26756",
			},
			&cli.BoolFlag{
				Name:    "pretend",
				EnvVars: []string{"PRETEND"},
				Value:   false,
			},
			&cli.BoolFlag{
				Name:    "verbose",
				EnvVars: []string{"VERBOSE"},
				Value:   false,
			},
		},
		Before: func(cc *cli.Context) error {
			if cc.Bool("verbose") {
				log.SetLevel(log.TraceLevel)
			}
			return nil
		},
	}
	err := app.Run(os.Args)
	var exitErr cli.ExitCoder
	if errors.As(err, &exitErr) {
		log.WithError(err).Error("application exited with error")
		os.Exit(exitErr.ExitCode())
	} else {
		log.WithError(err).Fatal("application exited with error")
	}
}
