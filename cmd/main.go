package main

import (
	"log"
	"net/http"
	_ "net/http/pprof"
	"os"
	"os/signal"
	"syscall"

	"github.com/contorno/eve-marketwatch/marketwatch"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

func main() {
	log.SetFlags(log.LstdFlags | log.Lshortfile)
	log.SetPrefix("eve-marketwatch: ")
	log.Println("starting eve-marketwatch")

	// Run MW.
	mw, err := marketwatch.NewMarketWatch()
	if err != nil {
		log.Fatal(err)
	}

	go func() {
		err := mw.Run()
		if err != nil {
			log.Fatalln("failed to run market watch server")
		}
		log.Println("started the market watch server on port 3005")
	}()

	// Run metrics endpoint.
	http.Handle("/metrics", promhttp.Handler())
	go func() {
		err := http.ListenAndServe(":3000", nil)
		if err != nil {
			log.Fatalln("failed to run metrics server")
		}
		log.Println("started the metrics server on port 3000")
	}()

	// Handle SIGINT and SIGTERM.
	ch := make(chan os.Signal, 1)
	signal.Notify(ch, syscall.SIGINT, syscall.SIGTERM)
	log.Println(<-ch)
}
