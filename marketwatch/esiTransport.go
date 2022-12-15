package marketwatch

import (
	"log"
	"net/http"
	"regexp"
	"strconv"
	"time"

	"github.com/prometheus/client_golang/prometheus"
)

var apiTransportLimiter chan bool
var urlFilterRe *regexp.Regexp

func init() {
	// concurrency limiter
	// 100 concurrent requests should fill 1 connection
	apiTransportLimiter = make(chan bool, 100)
	urlFilterRe = regexp.MustCompile("/v[0-9]/|/[0-9]+/")
}

// APITransport custom transport to chain into the HTTPClient to gather statistics.
type APITransport struct {
	next *http.Transport
}

// RoundTrip wraps http.DefaultTransport.RoundTrip to provide stats and handle error rates.
func (t *APITransport) RoundTrip(req *http.Request) (*http.Response, error) {
	// Limit concurrency
	apiTransportLimiter <- true

	// Free the worker
	defer func() { <-apiTransportLimiter }()

	tries := 0

	for {
		// Tick up retry counter
		tries++

		// Run the request and time the response
		start := time.Now()
		res, triperr := t.next.RoundTrip(req)
		end := time.Now()

		endpoint := urlFilterRe.ReplaceAllString(req.URL.Path, "/")

		// We got a response
		if res != nil {
			// Log metrics
			metricAPICalls.With(
				prometheus.Labels{
					"host":     req.Host,
					"endpoint": endpoint,
					"status":   strconv.Itoa(res.StatusCode),
					"try":      strconv.Itoa(tries),
				},
			).Observe(float64(end.Sub(start).Nanoseconds()) / float64(time.Millisecond))

			// Get the ESI error information
			limitReset := res.Header.Get("x-esi-error-limit-reset")
			limitRemain := res.Header.Get("x-esi-error-limit-remain")

			// If we cannot decode this is likely from another source.
			esiRateLimiter := true
			reset, err := strconv.ParseFloat(limitReset, 64)
			if err != nil {
				esiRateLimiter = false
			}
			remain, err := strconv.ParseFloat(limitRemain, 64)
			if err != nil {
				esiRateLimiter = false
			}

			// Tick up and log any errors
			if res.StatusCode >= 400 {
				metricAPIErrors.Inc()
				log.Printf(
					"Status: %d Reset: %s Remain: %s - %s\n",
					res.StatusCode,
					limitReset,
					limitRemain,
					req.URL,
				)

				var responsible string

				if res.StatusCode < 500 {
					responsible = "client"
				} else {
					responsible = "server"
				}

				log.Printf(
					"request failed due to %s error. Status code: %d.\nRequest URL: %s\nMessage: %s\n",
					responsible,
					res.StatusCode,
					req.URL,
					res.Status,
				)

				if esiRateLimiter {
					percentRemain := 1 - (remain / 100)
					duration := reset * percentRemain
					time.Sleep(time.Second * time.Duration(duration))
				} else {
					time.Sleep(time.Second * time.Duration(tries))
				}
			}

			if res.StatusCode >= 200 && res.StatusCode < 400 {
				return res, triperr
			}
		}

		if tries > 15 {
			log.Printf("too many tries, aborting\n")
			return res, triperr
		}
	}
}

var (
	metricAPICalls = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Namespace: "evemarketwatch",
			Subsystem: "api",
			Name:      "calls",
			Help:      "API call statistics.",
			Buckets:   prometheus.ExponentialBuckets(10, 1.45, 20),
		},
		[]string{"host", "status", "try", "endpoint"},
	)

	metricAPIErrors = prometheus.NewCounter(
		prometheus.CounterOpts{
			Namespace: "evemarketwatch",
			Subsystem: "api",
			Name:      "errors",
			Help:      "Count of API errors.",
		},
	)
)

func init() {
	prometheus.MustRegister(
		metricAPICalls,
		metricAPIErrors,
	)
}
