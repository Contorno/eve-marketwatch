package marketwatch

import (
	"log"
	"net/http"
	"os"
	"regexp"
	"strconv"
	"time"

	"github.com/prometheus/client_golang/prometheus"
)

var debugRaw = os.Getenv("DEBUG")
var apiTransportLimiter chan bool
var urlFilterRe *regexp.Regexp

func init() {
	// Concurrency limiter. 100 concurrent requests should fill 1 connection.
	apiTransportLimiter = make(chan bool, 100)
	urlFilterRe = regexp.MustCompile("/v[0-9]/|/[0-9]+/")
}

type APITransport struct {
	next *http.Transport
}

func logRoundTrip(req *http.Request, res *http.Response, reset int64, remain int64) {
	fmtStr := "\n\tRequest: %s\n\t\tQuery parameters: %v\n\t\tHeaders: %v\n\t\tStatus code: %d.\n\t\tStatus: %s." +
		"\n\t\tSeconds until error limit reset: %d\n\t\tError limit remaining: %d\n"
	fmtArgs := []any{
		req.Method + " " + req.URL.Path,
		req.URL.Query(),
		req.Header,
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

	if !debug && (res.StatusCode >= 200 && res.StatusCode < 400) {
		return
	}

	log.Printf(fmtStr, fmtArgs...)
}

// RoundTrip wraps http.DefaultTransport.RoundTrip to provide stats and handle error rates.
func (t *APITransport) RoundTrip(req *http.Request) (*http.Response, error) {
	// Limit concurrency
	apiTransportLimiter <- true
	defer func() { <-apiTransportLimiter }()

	tries := 0

	for {
		tries++

		// Run the request and time the response
		start := time.Now()
		res, triperr := t.next.RoundTrip(req)
		end := time.Now()

		endpoint := urlFilterRe.ReplaceAllString(req.URL.Path, "/")

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

			esiRateLimiter := true
			reset, err := strconv.ParseInt(limitReset, 10, 8)
			if err != nil {
				esiRateLimiter = false
			}
			remain, err := strconv.ParseInt(limitRemain, 10, 8)
			if err != nil {
				esiRateLimiter = false
			}

			if res.StatusCode >= 400 {
				metricAPIErrors.Inc()
				logRoundTrip(req, res, reset, remain)

				// do not retry 4xx errors
				if res.StatusCode >= 400 && res.StatusCode < 500 {
					return res, triperr
				}

				if esiRateLimiter {
					percentRemain := 1 - (remain / 100)
					duration := reset * percentRemain
					time.Sleep(time.Second * time.Duration(duration))
				} else {
					time.Sleep(time.Second * time.Duration(tries))
				}
			} else if res.StatusCode >= 200 && res.StatusCode < 400 {
				logRoundTrip(req, res, reset, remain)
				return res, triperr
			}
		}

		if tries > 5 {
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
