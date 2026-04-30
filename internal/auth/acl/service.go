package acl

import (
	"errors"
	"fmt"
	"strings"

	authjwt "github.com/opencorex/crabmq-core/internal/auth/jwt"
)

type Authorizer struct{}

func New() *Authorizer {
	return &Authorizer{}
}

func (a *Authorizer) CanPublish(claims authjwt.Claims, topic string) error {
	if claims.Role == "admin" {
		return nil
	}

	if claims.Role != "device" {
		return errors.New("unknown client role")
	}

	devicePrefix := fmt.Sprintf("device/%s/", claims.DeviceID)
	if !strings.HasPrefix(topic, devicePrefix) {
		return fmt.Errorf("device %s cannot publish to %s", claims.DeviceID, topic)
	}

	if strings.HasSuffix(topic, "/command") {
		return fmt.Errorf("device %s cannot publish command topics", claims.DeviceID)
	}

	return nil
}

func (a *Authorizer) CanSubscribe(claims authjwt.Claims, topic string) error {
	if claims.Role == "admin" {
		return nil
	}

	if claims.Role != "device" {
		return errors.New("unknown client role")
	}

	expected := fmt.Sprintf("device/%s/command", claims.DeviceID)
	if topic != expected {
		return fmt.Errorf("device %s cannot subscribe to %s", claims.DeviceID, topic)
	}

	return nil
}
