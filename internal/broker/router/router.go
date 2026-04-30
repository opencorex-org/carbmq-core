package router

import (
	"context"
	"log/slog"

	"github.com/opencorex/crabmq-core/internal/broker/session"
	"github.com/opencorex/crabmq-core/internal/broker/subscription"
	"github.com/opencorex/crabmq-core/internal/metrics"
	qtttypes "github.com/opencorex/crabmq-core/pkg/qtt/types"
)

type OfflineQueue interface {
	Enqueue(context.Context, string, qtttypes.Packet) error
}

type Router struct {
	sessions      *session.Store
	subscriptions *subscription.Registry
	offline       OfflineQueue
	metrics       *metrics.Registry
	logger        *slog.Logger
}

func New(
	sessions *session.Store,
	subscriptions *subscription.Registry,
	offline OfflineQueue,
	metricsRegistry *metrics.Registry,
	logger *slog.Logger,
) *Router {
	return &Router{
		sessions:      sessions,
		subscriptions: subscriptions,
		offline:       offline,
		metrics:       metricsRegistry,
		logger:        logger,
	}
}

func (r *Router) Route(ctx context.Context, packet qtttypes.Packet) (int, error) {
	matches := r.subscriptions.Match(packet.Topic)
	delivered := 0

	for _, match := range matches {
		target, ok := r.sessions.Get(match.ClientID)
		if !ok {
			continue
		}

		outbound := packet.Clone()
		if outbound.QoS > match.QoS {
			outbound.QoS = match.QoS
		}

		if target.Online && target.Conn != nil {
			if err := target.Conn.Send(ctx, outbound); err != nil {
				r.metrics.FailedDeliveries.Inc()
				r.logger.Warn("failed to deliver packet", "client_id", target.ClientID, "topic", packet.Topic, "error", err)
				if outbound.QoS > 0 {
					if queueErr := r.offline.Enqueue(ctx, target.ClientID, outbound); queueErr != nil {
						return delivered, queueErr
					}
					r.metrics.OfflineQueue.Inc()
				}
				continue
			}
			r.metrics.PacketsOut.Inc()
			delivered++
			continue
		}

		if outbound.QoS > 0 {
			if err := r.offline.Enqueue(ctx, target.ClientID, outbound); err != nil {
				return delivered, err
			}
			r.metrics.OfflineQueue.Inc()
		}
	}

	return delivered, nil
}
