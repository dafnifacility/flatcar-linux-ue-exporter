package main

import (
	"errors"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/acobaugh/osrelease"
	"github.com/coreos/go-systemd/daemon"
	"github.com/flatcar-linux/flatcar-linux-update-operator/pkg/updateengine"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	log "github.com/sirupsen/logrus"
	"github.com/urfave/cli/v2"

	"github.com/dafnifacility/flatcar-linux-ue-exporter/internal/pkg/html"
	"github.com/dafnifacility/flatcar-linux-ue-exporter/internal/pkg/kernel"
)

const (
	updateStatusPrefix = "UPDATE_STATUS_"
	metricNamespace    = "flatcar_linux"
	metricSubsystem    = "update_engine"
)

var (
	metricLastCheckedTimestampSeconds = promauto.NewGauge(prometheus.GaugeOpts{
		Namespace: metricNamespace,
		Subsystem: metricSubsystem,
		Name:      "last_checked_time_s",
	})
	metricLastDBUSUpdate = promauto.NewGauge(prometheus.GaugeOpts{
		Namespace: metricNamespace,
		Subsystem: metricSubsystem,
		Name:      "last_dbus_update",
	})
	metricHostStatus = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Namespace: metricNamespace,
		Subsystem: metricSubsystem,
		Name:      "status",
	}, []string{"op"})
	metricCurrentOSVersion = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Namespace: metricNamespace,
		Subsystem: metricSubsystem,
		Name:      "os",
	}, []string{"id", "version", "board"})
	metricCurrentKernelVersion = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Namespace: metricNamespace,
		Subsystem: metricSubsystem,
		Name:      "kernel",
	}, []string{"release"})
	metricCurrentUptime = promauto.NewGaugeFunc(prometheus.GaugeOpts{
		Namespace: metricNamespace,
		Subsystem: metricSubsystem,
		Name:      "system_uptime_s",
	}, func() float64 {
		up, _ := kernel.Uptime()
		return float64(up)
	})
	metricNewVersionAvailable = promauto.NewGauge(prometheus.GaugeOpts{
		Namespace: metricNamespace,
		Subsystem: metricSubsystem,
		Name:      "new_version_available",
	})
	metricUpdateProgress = promauto.NewGauge(prometheus.GaugeOpts{
		Namespace: metricNamespace,
		Subsystem: metricSubsystem,
		Name:      "update_progress_pct",
	})
)

func reqInfoFields(r *http.Request) log.Fields {
	return log.Fields{
		"client":     r.RemoteAddr,
		"user-agent": r.Header.Get("user-agent"),
	}
}

func logRequestHandler(inner http.Handler) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		log.WithFields(reqInfoFields(r)).Info(strings.Join([]string{r.Proto, r.Method, r.URL.Path}, " "))
		inner.ServeHTTP(w, r)
	}
}

var isReady = false

var respOK http.HandlerFunc = func(rw http.ResponseWriter, req *http.Request) {
	log.WithFields(reqInfoFields(req)).Debug(req.URL.Path)
	rw.WriteHeader(http.StatusNoContent)
}

func runWebServer(cc *cli.Context) {
	laddr := cc.String("listen-address")
	http.HandleFunc("/", logRequestHandler(http.FileServer(http.FS(html.Content))))
	http.HandleFunc("/metrics", logRequestHandler(promhttp.Handler()))
	http.HandleFunc("/healthz", respOK)
	http.HandleFunc("/readyz", func(rw http.ResponseWriter, req *http.Request) {
		if isReady {
			respOK(rw, req)
		} else {
			log.WithFields(reqInfoFields(req)).Warn("ready request but not connected to agent yet")
			rw.WriteHeader(http.StatusServiceUnavailable)
		}
	})
	log.WithField("listen-addr", laddr).Info("starting HTTP server for metrics")
	http.ListenAndServe(laddr, nil)
}

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
			metricHostStatus.WithLabelValues(astate[len(updateStatusPrefix):]).Set(1)
		} else {
			metricHostStatus.WithLabelValues(astate[len(updateStatusPrefix):]).Set(0)
		}
	}
}

func setupSystemd() {
	if isset, err := daemon.SdNotify(false, daemon.SdNotifyReady); isset {
		if err != nil {
			log.WithError(err).Error("systemd daemon notification error, process may be restarted/fail under systemd")
		} else {
			log.Info("systemd notified exporter is ready")
		}
	} else {
		log.Debug("not notifying manager due to no NOTIFY_SOCKET variable")
	}
	if interval, err := daemon.SdWatchdogEnabled(false); interval > 0 {
		if err != nil {
			log.WithError(err).Error("systemd watchdog setup error, process may be restarted by systemd")
		} else {
			log.Info("starting systemd watchdog handler")
			go func(t time.Duration) {
				for {
					log.Trace("systemd watchdog ping")
					time.Sleep(t / 2)
					daemon.SdNotify(false, daemon.SdNotifyWatchdog)
				}
			}(interval)
		}
	}
}

func getSystemRelease() error {
	rel, err := osrelease.Read()
	if err != nil {
		log.WithError(err).Warn("unable to get os-release from filesystem")
	} else {
		metricCurrentOSVersion.WithLabelValues(rel["ID"], rel["VERSION"], rel["FLATCAR_BOARD"]).Set(1)
	}
	kv, err := kernel.Version()
	if err != nil {
		log.WithError(err).Warning("unable to get kernel version")
	} else {
		metricCurrentKernelVersion.WithLabelValues(kv).Set(1)
	}
	return nil
}

func runExporter(cc *cli.Context) error {
	go runWebServer(cc)
	// We only check OS version once, because it can't change without a reboot anyway
	err := getSystemRelease()
	if err != nil {
		log.WithError(err).Warning("unable to get operating system version")
	}
	if !cc.Bool("pretend") {
		ue, err := updateengine.New()
		if err != nil {
			return err
		}
		us := make(chan updateengine.Status, 1)
		go ue.ReceiveStatuses(us, cc.Context.Done())
		setupSystemd()
		log.Debug("started update engine status client")
		// We now enter an infinite loop where we wait for update statuses, processing each one in-turn
		for err == nil {
			select {
			case st := <-us:
				isReady = true
				log.WithField("status", st).Debug("engine status update")
				metricLastDBUSUpdate.SetToCurrentTime()
				metricLastCheckedTimestampSeconds.Set(float64(st.LastCheckedTime))
				updateOpstatus(st.CurrentOperation)
				metricUpdateProgress.Set(st.Progress)
				if st.NewVersion != "0.0.0" {
					metricNewVersionAvailable.Set(1)
				} else {
					metricNewVersionAvailable.Set(0)
				}
			case <-cc.Context.Done():
				log.Info("shutting down update engine exporter")
				err = cli.Exit("normal exit", 0)
			}
		}
		return err
	} else {
		for {
			log.Warn("running in pretend mode - not actually checking with update engine!")
			updateOpstatus(updateengine.UpdateStatusIdle)
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
