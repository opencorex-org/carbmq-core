package services

import (
	"context"
	"time"

	"github.com/opencorex/crabmq-core/api/db"
	authjwt "github.com/opencorex/crabmq-core/internal/auth/jwt"
)

type DeviceService struct {
	store    *db.Store
	auth     *authjwt.Service
	tokenTTL time.Duration
}

func NewDeviceService(store *db.Store, auth *authjwt.Service, tokenTTL time.Duration) *DeviceService {
	return &DeviceService{
		store:    store,
		auth:     auth,
		tokenTTL: tokenTTL,
	}
}

func (s *DeviceService) Register(ctx context.Context, id string, name string, metadata map[string]any) (db.Device, error) {
	return s.store.RegisterDevice(ctx, id, name, metadata)
}

func (s *DeviceService) IssueToken(ctx context.Context, deviceID string) (string, error) {
	if _, err := s.store.GetDevice(ctx, deviceID); err != nil {
		return "", err
	}

	return s.auth.IssueDeviceToken(deviceID, s.tokenTTL)
}

func (s *DeviceService) List(ctx context.Context) ([]db.Device, error) {
	return s.store.ListDevices(ctx)
}

func (s *DeviceService) Telemetry(ctx context.Context, deviceID string, limit int) ([]db.Message, error) {
	return s.store.GetTelemetry(ctx, deviceID, limit)
}

func (s *DeviceService) Messages(ctx context.Context, limit int) ([]db.Message, error) {
	return s.store.ListMessages(ctx, limit)
}
