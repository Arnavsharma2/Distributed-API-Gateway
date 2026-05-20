package observability

import (
	"net/http"
	"strconv"
	"time"

	"github.com/aps/gatekeeper/internal/resilience"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

type Metrics struct {
	registry       *prometheus.Registry
	requests       *prometheus.CounterVec
	latency        *prometheus.HistogramVec
	cacheEvents    *prometheus.CounterVec
	rateLimited    *prometheus.CounterVec
	upstreamErrors *prometheus.CounterVec
	retries        *prometheus.CounterVec
	circuitState   *prometheus.GaugeVec
}

func NewMetrics() *Metrics {
	registry := prometheus.NewRegistry()
	m := &Metrics{
		registry: registry,
		requests: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "gatekeeper_requests_total",
			Help: "Total requests handled by the gateway.",
		}, []string{"route", "method", "status"}),
		latency: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Name:    "gatekeeper_request_duration_seconds",
			Help:    "Gateway request latency in seconds.",
			Buckets: []float64{0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2, 5},
		}, []string{"route", "method"}),
		cacheEvents: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "gatekeeper_cache_events_total",
			Help: "Cache events by route and result.",
		}, []string{"route", "result"}),
		rateLimited: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "gatekeeper_rate_limited_total",
			Help: "Requests rejected by rate limiting.",
		}, []string{"route"}),
		upstreamErrors: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "gatekeeper_upstream_errors_total",
			Help: "Upstream network errors and 5xx responses.",
		}, []string{"route"}),
		retries: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "gatekeeper_retries_total",
			Help: "Retry attempts issued by the gateway.",
		}, []string{"route"}),
		circuitState: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Name: "gatekeeper_circuit_state",
			Help: "Circuit breaker state by route. A value of 1 marks the active state.",
		}, []string{"route", "state"}),
	}
	registry.MustRegister(
		collectors.NewGoCollector(),
		collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}),
		m.requests,
		m.latency,
		m.cacheEvents,
		m.rateLimited,
		m.upstreamErrors,
		m.retries,
		m.circuitState,
	)
	return m
}

func (m *Metrics) Handler() http.Handler {
	return promhttp.HandlerFor(m.registry, promhttp.HandlerOpts{})
}

func (m *Metrics) RecordRequest(route string, method string, status int, duration time.Duration) {
	statusLabel := strconv.Itoa(status)
	m.requests.WithLabelValues(route, method, statusLabel).Inc()
	m.latency.WithLabelValues(route, method).Observe(duration.Seconds())
}

func (m *Metrics) RecordCache(route string, result string) {
	m.cacheEvents.WithLabelValues(route, result).Inc()
}

func (m *Metrics) RecordRateLimited(route string) {
	m.rateLimited.WithLabelValues(route).Inc()
}

func (m *Metrics) RecordUpstreamError(route string) {
	m.upstreamErrors.WithLabelValues(route).Inc()
}

func (m *Metrics) RecordRetry(route string) {
	m.retries.WithLabelValues(route).Inc()
}

func (m *Metrics) SetCircuitState(route string, state resilience.State) {
	states := []resilience.State{resilience.StateClosed, resilience.StateOpen, resilience.StateHalfOpen}
	for _, candidate := range states {
		value := 0.0
		if candidate == state {
			value = 1
		}
		m.circuitState.WithLabelValues(route, string(candidate)).Set(value)
	}
}
