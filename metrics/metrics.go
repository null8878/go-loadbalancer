package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

var (
	// RequestCount tracks total requests by method, status, and backend.
	RequestCount = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "gobalancer_requests_total",
			Help: "Total number of proxied requests",
		},
		[]string{"method", "status", "backend"},
	)

	// RequestDuration tracks request latency in seconds.
	RequestDuration = promauto.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "gobalancer_request_duration_seconds",
			Help:    "Request duration in seconds",
			Buckets: []float64{.005, .01, .025, .05, .1, .25, .5, 1, 2.5, 5},
		},
		[]string{"method", "backend"},
	)

	// ActiveConnections tracks the number of in-flight requests.
	ActiveConnections = promauto.NewGauge(
		prometheus.GaugeOpts{
			Name: "gobalancer_active_connections",
			Help: "Number of active connections being proxied",
		},
	)

	// BackendHealth tracks whether a backend is healthy (1) or unhealthy (0).
	BackendHealth = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "gobalancer_backend_health",
			Help: "Health status of each backend (1=healthy, 0=unhealthy)",
		},
		[]string{"backend"},
	)

	// RateLimitRejections counts requests rejected by the rate limiter.
	RateLimitRejections = promauto.NewCounter(
		prometheus.CounterOpts{
			Name: "gobalancer_rate_limit_rejections_total",
			Help: "Total number of requests rejected by rate limiter",
		},
	)
)
