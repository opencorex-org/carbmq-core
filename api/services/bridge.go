package services

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/opencorex/crabmq-core/api/db"
	authjwt "github.com/opencorex/crabmq-core/internal/auth/jwt"
	"github.com/opencorex/crabmq-core/internal/config"
	qttclient "github.com/opencorex/crabmq-core/pkg/qtt/client"
	qtttypes "github.com/opencorex/crabmq-core/pkg/qtt/types"
	"github.com/opencorex/crabmq-core/pkg/qtt/utils"
)

type Bridge struct {
	cfg    config.Config
	store  *db.Store
	auth   *authjwt.Service
	hub    *Hub
	logger *slog.Logger

	mu     sync.RWMutex
	client *qttclient.Client
}

func NewBridge(cfg config.Config, store *db.Store, auth *authjwt.Service, logger *slog.Logger) *Bridge {
	return &Bridge{
		cfg:    cfg,
		store:  store,
		auth:   auth,
		hub:    NewHub(logger),
		logger: logger,
	}
}

func (b *Bridge) Hub() *Hub {
	return b.hub
}

func (b *Bridge) Start(ctx context.Context) error {
	backoff := 2 * time.Second

	for ctx.Err() == nil {
		token, err := b.auth.IssueAdminToken(b.cfg.API.BridgeClientID, b.cfg.Auth.AdminTokenTTL)
		if err != nil {
			return err
		}

		client, err := qttclient.Dial(ctx, qttclient.Config{
			Addr:         b.cfg.API.BrokerAddr,
			ClientID:     b.cfg.API.BridgeClientID,
			Token:        token,
			ProtocolName: b.cfg.Broker.ProtocolName,
		})
		if err != nil {
			b.logger.Warn("api bridge dial failed", "error", err)
			if !sleepContext(ctx, backoff) {
				return nil
			}
			continue
		}

		if _, err := client.Connect(ctx, map[string]string{"role": "bridge"}); err != nil {
			_ = client.Close()
			b.logger.Warn("api bridge connect failed", "error", err)
			if !sleepContext(ctx, backoff) {
				return nil
			}
			continue
		}

		if err := client.Subscribe(ctx, []qtttypes.Subscription{{Topic: "device/+/telemetry", QoS: 1}}); err != nil {
			_ = client.Close()
			b.logger.Warn("api bridge subscribe failed", "error", err)
			if !sleepContext(ctx, backoff) {
				return nil
			}
			continue
		}

		subAck, err := client.ReadPacket(ctx)
		if err != nil {
			_ = client.Close()
			b.logger.Warn("api bridge suback failed", "error", err)
			if !sleepContext(ctx, backoff) {
				return nil
			}
			continue
		}
		if subAck.Type != qtttypes.PacketSubAck {
			_ = client.Close()
			b.logger.Warn("api bridge expected SUBACK", "packet_type", subAck.Type)
			if !sleepContext(ctx, backoff) {
				return nil
			}
			continue
		}

		b.setClient(client)
		b.logger.Info("api bridge connected to broker", "broker_addr", b.cfg.API.BrokerAddr)
		err = b.consume(ctx, client)
		b.clearClient(client)
		_ = client.Close()
		if err != nil && !errors.Is(err, context.Canceled) {
			b.logger.Warn("api bridge disconnected", "error", err)
		}
		if !sleepContext(ctx, backoff) {
			return nil
		}
	}

	return nil
}

func (b *Bridge) PublishCommand(ctx context.Context, deviceID string, payload []byte) (qtttypes.Packet, error) {
	client := b.currentClient()
	if client == nil {
		return qtttypes.Packet{}, errors.New("bridge is not connected to broker")
	}

	packet := qtttypes.Packet{
		Type:      qtttypes.PacketPublish,
		PacketID:  utils.NewPacketID(),
		ClientID:  b.cfg.API.BridgeClientID,
		Topic:     fmt.Sprintf("device/%s/command", deviceID),
		QoS:       b.cfg.API.CommandQoS,
		Payload:   payload,
		Timestamp: utils.TimestampNow(),
	}

	if err := client.Send(ctx, packet); err != nil {
		return qtttypes.Packet{}, err
	}

	return packet, nil
}

func (b *Bridge) consume(ctx context.Context, client *qttclient.Client) error {
	for {
		packet, err := client.ReadPacket(ctx)
		if err != nil {
			return err
		}

		switch packet.Type {
		case qtttypes.PacketPublish:
			b.hub.Broadcast(TelemetryEvent{
				PacketID:  packet.PacketID,
				DeviceID:  utils.TopicOwner(packet.Topic),
				Topic:     packet.Topic,
				QoS:       packet.QoS,
				Timestamp: packet.Timestamp,
				Payload:   packet.Payload,
			})

			if packet.QoS > 0 {
				if err := client.SendPubAck(ctx, packet.PacketID); err != nil {
					return err
				}
			}
		case qtttypes.PacketError:
			b.logger.Warn("bridge received broker error", "code", packet.Code, "error", packet.Error)
		}
	}
}

func (b *Bridge) currentClient() *qttclient.Client {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.client
}

func (b *Bridge) setClient(client *qttclient.Client) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.client = client
}

func (b *Bridge) clearClient(client *qttclient.Client) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.client == client {
		b.client = nil
	}
}

func sleepContext(ctx context.Context, duration time.Duration) bool {
	timer := time.NewTimer(duration)
	defer timer.Stop()

	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}
