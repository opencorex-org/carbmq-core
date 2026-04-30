package jwt

import (
	"errors"
	"fmt"
	"time"

	gjwt "github.com/golang-jwt/jwt/v5"

	"github.com/opencorex/crabmq-core/internal/config"
)

type Claims struct {
	ClientID string `json:"clientId"`
	DeviceID string `json:"deviceId,omitempty"`
	Role     string `json:"role"`
	gjwt.RegisteredClaims
}

type Service struct {
	secret    []byte
	adminTTL  time.Duration
	deviceTTL time.Duration
}

func New(cfg config.AuthConfig) *Service {
	return &Service{
		secret:    []byte(cfg.JWTSecret),
		adminTTL:  cfg.AdminTokenTTL,
		deviceTTL: cfg.DeviceTokenTTL,
	}
}

func (s *Service) Parse(token string) (Claims, error) {
	if token == "" {
		return Claims{}, errors.New("missing token")
	}

	parsed, err := gjwt.ParseWithClaims(token, &Claims{}, func(parsed *gjwt.Token) (any, error) {
		if _, ok := parsed.Method.(*gjwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method %T", parsed.Method)
		}

		return s.secret, nil
	})
	if err != nil {
		return Claims{}, fmt.Errorf("parse jwt: %w", err)
	}

	claims, ok := parsed.Claims.(*Claims)
	if !ok || !parsed.Valid {
		return Claims{}, errors.New("invalid token")
	}

	return *claims, nil
}

func (s *Service) IssueDeviceToken(deviceID string, ttl time.Duration) (string, error) {
	if ttl <= 0 {
		ttl = s.deviceTTL
	}

	return s.issueToken(deviceID, deviceID, "device", ttl)
}

func (s *Service) IssueAdminToken(clientID string, ttl time.Duration) (string, error) {
	if ttl <= 0 {
		ttl = s.adminTTL
	}

	return s.issueToken(clientID, "", "admin", ttl)
}

func (s *Service) issueToken(clientID string, deviceID string, role string, ttl time.Duration) (string, error) {
	now := time.Now().UTC()
	claims := Claims{
		ClientID: clientID,
		DeviceID: deviceID,
		Role:     role,
		RegisteredClaims: gjwt.RegisteredClaims{
			Subject:   clientID,
			IssuedAt:  gjwt.NewNumericDate(now),
			ExpiresAt: gjwt.NewNumericDate(now.Add(ttl)),
		},
	}

	token := gjwt.NewWithClaims(gjwt.SigningMethodHS256, claims)
	signed, err := token.SignedString(s.secret)
	if err != nil {
		return "", fmt.Errorf("sign jwt: %w", err)
	}

	return signed, nil
}
