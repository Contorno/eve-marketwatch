package marketwatch

import (
	"fmt"
	"log"
	"net/http"
	"os"
	"regexp"
	"strconv"
	"time"

	"github.com/prometheus/client_golang/prometheus"
)

// debug server
var debugRaw = os.Getenv("DEBUG")

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

func logRoundTrip(req *http.Request, res *http.Response, reset float64, remain float64) {
	fmtStr := "\n\tRequest: %s\n\t\tQuery parameters: %v\n\t\tStatus code: %d.\n\t\tStatus: %s." +
		"\n\t\tError limit reset: %f\n\t\tError limit remaining: %f\n"
	fmtArgs := []any{
		req.Method + " " + req.URL.Path,
		req.URL.Query(),
		res.StatusCode,
		res.Status,
		reset,
		remain,
	}

	var debug bool
	if debugRaw == "true" {
		debug = true
	} else {
		debug = false
	}

	if debug {
		fmtArgs = append(fmtArgs, fmt.Sprintf("%v", req.Header))
		fmtStr += "\t\tHeaders: %s\n"
	} else if !debug && (res.StatusCode >= 200 && res.StatusCode < 400) {
		return
	}

	log.Printf(fmtStr, fmtArgs...)
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
				logRoundTrip(req, res, reset, remain)

				if esiRateLimiter {
					percentRemain := 1 - (remain / 100)
					duration := reset * percentRemain
					time.Sleep(time.Second * time.Duration(duration))
				} else {
					time.Sleep(time.Second * time.Duration(tries))
				}
			}

			if res.StatusCode >= 200 && res.StatusCode < 400 {
				logRoundTrip(req, res, reset, remain)
				return res, nil
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
