package metrics

import (
	"net/http"
	"strconv"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

type Registry struct {
	registry         *prometheus.Registry
	httpRequests     *prometheus.CounterVec
	httpDuration     *prometheus.HistogramVec
	transfers        *prometheus.CounterVec
	deliveries       *prometheus.CounterVec
	published        prometheus.Counter
	payments         *prometheus.CounterVec
	providerRequests *prometheus.CounterVec
}

func NewRegistry() *Registry {
	r := &Registry{
		registry:     prometheus.NewRegistry(),
		httpRequests: newCounter("paygo_http_requests_total", "HTTP requests served, by method, route and status.", "method", "route", "status"),
		httpDuration: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Name:    "paygo_http_request_duration_seconds",
			Help:    "HTTP request duration in seconds, by method and route.",
			Buckets: prometheus.DefBuckets,
		}, []string{"method", "route"}),
		transfers:  newCounter("paygo_transfers_total", "Money movements attempted, by kind and result.", "kind", "result"),
		deliveries: newCounter("paygo_webhook_deliveries_total", "Webhook delivery attempts, by result.", "result"),
		published: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "paygo_outbox_events_published_total",
			Help: "Outbox events fanned out to webhook deliveries.",
		}),
		payments:         newCounter("paygo_payments_total", "Pix charges and payouts created, by kind and status.", "kind", "status"),
		providerRequests: newCounter("paygo_provider_requests_total", "Payment provider calls, by provider, operation and result.", "provider", "operation", "result"),
	}
	r.registry.MustRegister(
		collectors.NewGoCollector(),
		collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}),
		r.httpRequests, r.httpDuration, r.transfers, r.deliveries, r.published, r.payments, r.providerRequests,
	)
	return r
}

func newCounter(name, help string, labels ...string) *prometheus.CounterVec {
	return prometheus.NewCounterVec(prometheus.CounterOpts{Name: name, Help: help}, labels)
}

func (r *Registry) Handler() http.Handler {
	return promhttp.HandlerFor(r.registry, promhttp.HandlerOpts{})
}

func (r *Registry) ObserveRequest(method, route string) func(status int, duration time.Duration) {
	return func(status int, duration time.Duration) {
		r.httpRequests.WithLabelValues(method, route, strconv.Itoa(status)).Inc()
		r.httpDuration.WithLabelValues(method, route).Observe(duration.Seconds())
	}
}
