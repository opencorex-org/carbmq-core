package server

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"time"

	"github.com/opencorex/crabmq-core/api/db"
	"github.com/opencorex/crabmq-core/internal/auth/acl"
	authjwt "github.com/opencorex/crabmq-core/internal/auth/jwt"
	"github.com/opencorex/crabmq-core/internal/broker/router"
	"github.com/opencorex/crabmq-core/internal/broker/session"
	"github.com/opencorex/crabmq-core/internal/broker/subscription"
	"github.com/opencorex/crabmq-core/internal/config"
	"github.com/opencorex/crabmq-core/internal/metrics"
	"github.com/opencorex/crabmq-core/internal/protocol/packets"
	"github.com/opencorex/crabmq-core/internal/queue/persistent"
	"github.com/opencorex/crabmq-core/internal/transport/connection"
	quictransport "github.com/opencorex/crabmq-core/internal/transport/quic"
	qtttypes "github.com/opencorex/crabmq-core/pkg/qtt/types"
	"github.com/opencorex/crabmq-core/pkg/qtt/utils"
)

type Server struct {
	cfg           config.Config
	store         *db.Store
	auth          *authjwt.Service
	acl           *acl.Authorizer
	sessions      *session.Store
	subscriptions *subscription.Registry
	router        *router.Router
	queue         *persistent.Store
	metrics       *metrics.Registry
	transport     *quictransport.Server
	limiter       *rateLimiter
	logger        *slog.Logger
}

func New(cfg config.Config, store *db.Store, logger *slog.Logger) *Server {
	metricsRegistry := metrics.New()
	sessions := session.NewStore()
	subscriptions := subscription.NewRegistry()
	queue := persistent.NewStore(store, cfg.Broker.OfflineQueueTTL)

	return &Server{
		cfg:           cfg,
		store:         store,
		auth:          authjwt.New(cfg.Auth),
		acl:           acl.New(),
		sessions:      sessions,
		subscriptions: subscriptions,
		queue:         queue,
		metrics:       metricsRegistry,
		transport:     quictransport.NewServer(cfg.Broker, logger),
		limiter:       newRateLimiter(cfg.Broker.RateLimitPerMin, time.Minute),
		logger:        logger,
		router:        router.New(sessions, subscriptions, queue, metricsRegistry, logger),
	}
}

func (s *Server) Metrics() *metrics.Registry {
	return s.metrics
}

func (s *Server) Run(ctx context.Context) error {
	go func() {
		if err := s.metrics.Start(ctx, s.cfg.Broker.MetricsAddr, s.logger); err != nil {
			s.logger.Error("metrics server stopped", "error", err)
		}
	}()

	return s.transport.Listen(ctx, s.handleConnection)
}

func (s *Server) handleConnection(ctx context.Context, conn connection.PacketConn) {
	defer func() {
		_ = conn.Close()
	}()

	clientID, claims, existingSubscriptions, err := s.authenticate(ctx, conn)
	if err != nil {
		s.metrics.AuthFailures.Inc()
		s.logger.Warn("connection rejected", "remote", conn.RemoteAddr(), "error", err)
		_ = conn.Send(ctx, packets.Error("AUTH_FAILED", err.Error()))
		return
	}
	defer func() {
		s.sessions.Disconnect(clientID)
		s.metrics.ActiveSessions.Set(float64(s.sessions.OnlineCount()))
	}()

	if err := s.deliverOffline(ctx, clientID, conn); err != nil {
		s.logger.Warn("failed to deliver offline queue", "client_id", clientID, "error", err)
	}
	if len(existingSubscriptions) > 0 {
		s.logger.Info("restored subscriptions", "client_id", clientID, "count", len(existingSubscriptions))
	}

	for {
		packet, err := conn.Receive(ctx)
		if err != nil {
			if !errors.Is(err, io.EOF) {
				s.logger.Info("connection closed", "client_id", clientID, "error", err)
			}
			return
		}

		s.metrics.PacketsIn.Inc()
		s.sessions.Touch(clientID)

		if !s.limiter.Allow(clientID) {
			s.metrics.RateLimited.Inc()
			_ = conn.Send(ctx, packets.Error("RATE_LIMITED", "too many packets"))
			continue
		}

		switch packet.Type {
		case qtttypes.PacketPing:
			if err := conn.Send(ctx, packets.Pong()); err != nil {
				return
			}
			s.metrics.PacketsOut.Inc()
		case qtttypes.PacketSubscribe:
			if err := s.handleSubscribe(ctx, conn, clientID, claims, packet); err != nil {
				_ = conn.Send(ctx, packets.Error("SUBSCRIBE_FAILED", err.Error()))
			}
		case qtttypes.PacketPublish:
			if err := s.handlePublish(ctx, conn, clientID, claims, packet); err != nil {
				_ = conn.Send(ctx, packets.Error("PUBLISH_FAILED", err.Error()))
			}
		case qtttypes.PacketPubAck:
			if err := s.store.MarkMessageAcknowledged(ctx, packet.PacketID); err != nil {
				s.logger.Warn("failed to mark message acked", "packet_id", packet.PacketID, "error", err)
			}
			if err := s.queue.Ack(ctx, packet.PacketID); err != nil {
				s.logger.Warn("failed to clear offline message", "packet_id", packet.PacketID, "error", err)
			} else {
				s.metrics.OfflineQueue.Dec()
			}
		case qtttypes.PacketDisconnect:
			return
		default:
			_ = conn.Send(ctx, packets.Error("UNSUPPORTED_PACKET", fmt.Sprintf("unsupported packet type %s", packet.Type)))
		}
	}
}

func (s *Server) authenticate(ctx context.Context, conn connection.PacketConn) (string, authjwt.Claims, []qtttypes.Subscription, error) {
	packet, err := conn.Receive(ctx)
	if err != nil {
		return "", authjwt.Claims{}, nil, fmt.Errorf("read connect packet: %w", err)
	}
	if packet.Type != qtttypes.PacketConnect {
		return "", authjwt.Claims{}, nil, errors.New("first packet must be CONNECT")
	}

	claims, err := s.auth.Parse(packet.Token)
	if err != nil {
		return "", authjwt.Claims{}, nil, err
	}

	clientID := packet.ClientID
	if clientID == "" {
		clientID = claims.ClientID
	}
	if clientID == "" {
		return "", authjwt.Claims{}, nil, errors.New("missing client id")
	}

	if claims.Role == "device" && claims.DeviceID != clientID {
		return "", authjwt.Claims{}, nil, fmt.Errorf("device token client mismatch: %s != %s", claims.DeviceID, clientID)
	}

	sessionPresent, subscriptions := s.sessions.UpsertConnected(session.Session{
		ClientID:    clientID,
		DeviceID:    claims.DeviceID,
		Role:        claims.Role,
		Claims:      claims,
		Conn:        conn,
		ConnectedAt: utils.TimestampNow(),
		LastSeenAt:  utils.TimestampNow(),
	})
	s.metrics.ActiveSessions.Set(float64(s.sessions.OnlineCount()))

	if err := conn.Send(ctx, packets.ConnAck("accepted", sessionPresent)); err != nil {
		return "", authjwt.Claims{}, nil, fmt.Errorf("send connack: %w", err)
	}
	s.metrics.PacketsOut.Inc()

	return clientID, claims, subscriptions, nil
}

func (s *Server) handleSubscribe(
	ctx context.Context,
	conn connection.PacketConn,
	clientID string,
	claims authjwt.Claims,
	packet qtttypes.Packet,
) error {
	if len(packet.Subscriptions) == 0 {
		return errors.New("missing subscriptions")
	}

	for _, subscription := range packet.Subscriptions {
		if err := s.acl.CanSubscribe(claims, subscription.Topic); err != nil {
			return err
		}
	}

	s.subscriptions.Add(clientID, packet.Subscriptions)
	s.sessions.AddSubscriptions(clientID, packet.Subscriptions)
	s.metrics.Subscriptions.Add(float64(len(packet.Subscriptions)))

	if err := conn.Send(ctx, packets.SubAck(packet.PacketID)); err != nil {
		return err
	}
	s.metrics.PacketsOut.Inc()
	return nil
}

func (s *Server) handlePublish(
	ctx context.Context,
	conn connection.PacketConn,
	clientID string,
	claims authjwt.Claims,
	packet qtttypes.Packet,
) error {
	if packet.Topic == "" {
		return errors.New("missing topic")
	}
	if err := s.acl.CanPublish(claims, packet.Topic); err != nil {
		return err
	}

	if packet.PacketID == "" {
		packet.PacketID = utils.NewPacketID()
	}
	if packet.Timestamp.IsZero() {
		packet.Timestamp = utils.TimestampNow()
	}
	packet.ClientID = clientID

	if err := s.store.SaveMessage(ctx, db.Message{
		PacketID:     packet.PacketID,
		ClientID:     clientID,
		Topic:        packet.Topic,
		QoS:          packet.QoS,
		Payload:      packet.Payload,
		PacketType:   string(packet.Type),
		Direction:    "inbound",
		Timestamp:    packet.Timestamp,
		Acknowledged: packet.QoS == 0,
	}); err != nil {
		return err
	}

	s.metrics.Publishes.Inc()
	if _, err := s.router.Route(ctx, packet); err != nil {
		return err
	}

	if packet.QoS > 0 {
		if err := conn.Send(ctx, packets.PubAck(packet.PacketID)); err != nil {
			return err
		}
		s.metrics.PacketsOut.Inc()
	}

	return nil
}

func (s *Server) deliverOffline(ctx context.Context, clientID string, conn connection.PacketConn) error {
	packetsPending, err := s.queue.Pending(ctx, clientID)
	if err != nil {
		return err
	}

	for _, packet := range packetsPending {
		if err := conn.Send(ctx, packet); err != nil {
			return err
		}
		s.metrics.PacketsOut.Inc()
	}

	return nil
}
