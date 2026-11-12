package main

import (
	"net/http"
	"strconv"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

var (
	registry = prometheus.NewRegistry()
	requests = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "graphql_requests_total", Help: "GraphQL HTTP requests by response class.",
	}, []string{"code"})
	latency = prometheus.NewHistogram(prometheus.HistogramOpts{
		Name: "graphql_request_duration_seconds", Help: "GraphQL request latency.",
		Buckets: prometheus.ExponentialBuckets(0.001, 2, 12),
	})
	gqlErrors = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "graphql_responses_with_errors_total", Help: "Responses whose body carries a GraphQL errors array.",
	})
)

func init() { registry.MustRegister(requests, latency, gqlErrors) }

type recorder struct {
	http.ResponseWriter
	code int
}

func (r *recorder) WriteHeader(c int) { r.code = c; r.ResponseWriter.WriteHeader(c) }

func instrument(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rec := &recorder{ResponseWriter: w, code: http.StatusOK}
		next.ServeHTTP(rec, r)
		latency.Observe(time.Since(start).Seconds())
		requests.WithLabelValues(strconv.Itoa(rec.code/100) + "xx").Inc()
	})
}

func metricsHandler() http.Handler { return promhttp.HandlerFor(registry, promhttp.HandlerOpts{}) }
