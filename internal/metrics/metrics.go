package metrics

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

type Registry struct {
	registry         *prometheus.Registry
	PacketsIn        prometheus.Counter
	PacketsOut       prometheus.Counter
	Publishes        prometheus.Counter
	Subscriptions    prometheus.Counter
	AuthFailures     prometheus.Counter
	RateLimited      prometheus.Counter
	FailedDeliveries prometheus.Counter
	ActiveSessions   prometheus.Gauge
	OfflineQueue     prometheus.Gauge
}

func New() *Registry {
	registry := prometheus.NewRegistry()

	packetsIn := prometheus.NewCounter(prometheus.CounterOpts{
		Namespace: "crabmq",
		Subsystem: "broker",
		Name:      "packets_in_total",
		Help:      "Number of QTT packets received by the broker.",
	})
	packetsOut := prometheus.NewCounter(prometheus.CounterOpts{
		Namespace: "crabmq",
		Subsystem: "broker",
		Name:      "packets_out_total",
		Help:      "Number of QTT packets sent by the broker.",
	})
	publishes := prometheus.NewCounter(prometheus.CounterOpts{
		Namespace: "crabmq",
		Subsystem: "broker",
		Name:      "publishes_total",
		Help:      "Number of publish packets accepted by the broker.",
	})
	subscriptions := prometheus.NewCounter(prometheus.CounterOpts{
		Namespace: "crabmq",
		Subsystem: "broker",
		Name:      "subscriptions_total",
		Help:      "Number of subscription requests processed by the broker.",
	})
	authFailures := prometheus.NewCounter(prometheus.CounterOpts{
		Namespace: "crabmq",
		Subsystem: "broker",
		Name:      "auth_failures_total",
		Help:      "Number of rejected authentication attempts.",
	})
	rateLimited := prometheus.NewCounter(prometheus.CounterOpts{
		Namespace: "crabmq",
		Subsystem: "broker",
		Name:      "rate_limited_total",
		Help:      "Number of packets rejected due to rate limiting.",
	})
	failedDeliveries := prometheus.NewCounter(prometheus.CounterOpts{
		Namespace: "crabmq",
		Subsystem: "broker",
		Name:      "failed_deliveries_total",
		Help:      "Number of routed packets that could not be delivered.",
	})
	activeSessions := prometheus.NewGauge(prometheus.GaugeOpts{
		Namespace: "crabmq",
		Subsystem: "broker",
		Name:      "active_sessions",
		Help:      "Current number of online client sessions.",
	})
	offlineQueue := prometheus.NewGauge(prometheus.GaugeOpts{
		Namespace: "crabmq",
		Subsystem: "broker",
		Name:      "offline_queue_depth",
		Help:      "Current depth of the persistent offline queue.",
	})

	registry.MustRegister(
		packetsIn,
		packetsOut,
		publishes,
		subscriptions,
		authFailures,
		rateLimited,
		failedDeliveries,
		activeSessions,
		offlineQueue,
	)

	return &Registry{
		registry:         registry,
		PacketsIn:        packetsIn,
		PacketsOut:       packetsOut,
		Publishes:        publishes,
		Subscriptions:    subscriptions,
		AuthFailures:     authFailures,
		RateLimited:      rateLimited,
		FailedDeliveries: failedDeliveries,
		ActiveSessions:   activeSessions,
		OfflineQueue:     offlineQueue,
	}
}

func (r *Registry) Handler() http.Handler {
	return promhttp.HandlerFor(r.registry, promhttp.HandlerOpts{})
}

func (r *Registry) Start(ctx context.Context, addr string, logger *slog.Logger) error {
	server := &http.Server{
		Addr:              addr,
		Handler:           r.Handler(),
		ReadHeaderTimeout: 5 * time.Second,
	}

	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = server.Shutdown(shutdownCtx)
	}()

	logger.Info("metrics endpoint listening", "addr", addr)
	if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		return fmt.Errorf("metrics server: %w", err)
	}

	return nil
}
