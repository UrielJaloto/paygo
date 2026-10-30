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
	registry     *prometheus.Registry
	httpRequests *prometheus.CounterVec
	httpDuration *prometheus.HistogramVec
	transfers    *prometheus.CounterVec
	deliveries   *prometheus.CounterVec
	published    prometheus.Counter
}

func NewRegistry() *Registry {
	r := &Registry{
		registry: prometheus.NewRegistry(),
		httpRequests: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "paygo_http_requests_total",
			Help: "HTTP requests served, by method, route and status.",
		}, []string{"method", "route", "status"}),
		httpDuration: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Name:    "paygo_http_request_duration_seconds",
			Help:    "HTTP request duration in seconds, by method and route.",
			Buckets: prometheus.DefBuckets,
		}, []string{"method", "route"}),
		transfers: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "paygo_transfers_total",
			Help: "Money movements attempted, by kind and result.",
		}, []string{"kind", "result"}),
		deliveries: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "paygo_webhook_deliveries_total",
			Help: "Webhook delivery attempts, by result.",
		}, []string{"result"}),
		published: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "paygo_outbox_events_published_total",
			Help: "Outbox events fanned out to webhook deliveries.",
		}),
	}
	r.registry.MustRegister(
		collectors.NewGoCollector(),
		collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}),
		r.httpRequests, r.httpDuration, r.transfers, r.deliveries, r.published,
	)
	return r
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
