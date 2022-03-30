package main

import (
	"errors"
	"net/http"
	"os"
	"time"

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

func runWebServer(cc *cli.Context) {
	laddr := cc.String("listen-address")
	http.Handle("/metrics", promhttp.Handler())
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
