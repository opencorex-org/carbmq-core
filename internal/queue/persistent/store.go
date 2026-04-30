package persistent

import (
	"context"
	"time"

	"github.com/opencorex/crabmq-core/api/db"
	qtttypes "github.com/opencorex/crabmq-core/pkg/qtt/types"
	"github.com/opencorex/crabmq-core/pkg/qtt/utils"
)

type Store struct {
	repo *db.Store
	ttl  time.Duration
}

func NewStore(repo *db.Store, ttl time.Duration) *Store {
	return &Store{
		repo: repo,
		ttl:  ttl,
	}
}

func (s *Store) Enqueue(ctx context.Context, clientID string, packet qtttypes.Packet) error {
	now := utils.TimestampNow()
	return s.repo.EnqueueOfflineMessage(ctx, db.OfflineMessage{
		PacketID:    packet.PacketID,
		ClientID:    clientID,
		Topic:       packet.Topic,
		QoS:         packet.QoS,
		Payload:     packet.Payload,
		EnqueuedAt:  now,
		AvailableAt: now,
	})
}

func (s *Store) Pending(ctx context.Context, clientID string) ([]qtttypes.Packet, error) {
	items, err := s.repo.GetOfflineMessages(ctx, clientID)
	if err != nil {
		return nil, err
	}

	packets := make([]qtttypes.Packet, 0, len(items))
	for _, item := range items {
		packets = append(packets, qtttypes.Packet{
			Type:      qtttypes.PacketPublish,
			PacketID:  item.PacketID,
			Topic:     item.Topic,
			QoS:       item.QoS,
			Payload:   item.Payload,
			Timestamp: item.EnqueuedAt,
		})
	}

	return packets, nil
}

func (s *Store) Ack(ctx context.Context, packetID string) error {
	return s.repo.DeleteOfflineMessage(ctx, packetID)
}
