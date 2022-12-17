package main

import (
	"log"
	"net/http"
	_ "net/http/pprof" //nolint:gosec
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/contorno/eve-marketwatch/marketwatch"
	"github.com/getsentry/sentry-go"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

const version = "eve-marketwatch@0.0.4"

func main() {
	log.SetFlags(log.LstdFlags | log.Lshortfile)
	log.SetPrefix("eve-marketwatch: ")
	log.Println("starting eve-marketwatch")
	dsn := os.Getenv("SENTRY_DSN")

	err := sentry.Init(
		sentry.ClientOptions{
			Release:          version,
			AttachStacktrace: true,
			Dsn:              dsn,
			EnableTracing:    true,
			Debug:            true,
			// Set TracesSampleRate to 1.0 to capture 100%
			// of transactions for performance monitoring.
			// Adjust this value in production.
			TracesSampleRate: 1.0,
		},
	)
	if err != nil {
		sentry.CaptureException(err)
		log.Fatalf("sentry.Init: %s", err)
	}

	mw, err := marketwatch.NewMarketWatch()
	if err != nil {
		sentry.CaptureException(err)
		log.Fatalln(err)
	}

	go func(localHub *sentry.Hub) {
		localHub.ConfigureScope(
			func(scope *sentry.Scope) {
				scope.SetTag("locationHash", "go#run-mw")
			},
		)
		err := mw.Run()

		if err != nil {
			sentry.CaptureException(err)
			log.Fatalln("failed to run market watch server")
		}
		log.Println("started the market watch server on port 3005")
	}(sentry.CurrentHub().Clone())

	http.Handle("/metrics", promhttp.Handler())
	go func(localHub *sentry.Hub) {
		localHub.ConfigureScope(
			func(scope *sentry.Scope) {
				scope.SetTag("locationHash", "go#serve-prometheus-metrics")
			},
		)

		err := http.ListenAndServe(":3000", nil) //nolint:gosec
		if err != nil {
			sentry.CaptureException(err)
			log.Fatalln("failed to run metrics server")
		}
		log.Println("started the metrics server on port 3000")
	}(sentry.CurrentHub().Clone())
	ch := make(chan os.Signal, 1)
	signal.Notify(ch, syscall.SIGINT, syscall.SIGTERM)
	log.Println(<-ch)
	defer sentry.Flush(3 * time.Second)
}
