package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"math/rand"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	qttclient "github.com/opencorex/crabmq-core/pkg/qtt/client"
	qtttypes "github.com/opencorex/crabmq-core/pkg/qtt/types"
)

type simulatorConfig struct {
	Count        int
	BrokerAddr   string
	APIBaseURL   string
	DevicePrefix string
	Interval     time.Duration
}

func main() {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	cfg := simulatorConfig{
		Count:        envInt("CRABMQ_SIM_DEVICE_COUNT", 50),
		BrokerAddr:   env("CRABMQ_BROKER_ADDR", "127.0.0.1:1884"),
		APIBaseURL:   env("CRABMQ_API_URL", "http://127.0.0.1:8080"),
		DevicePrefix: env("CRABMQ_SIM_PREFIX", "sim-device"),
		Interval:     envDuration("CRABMQ_SIM_INTERVAL", 5*time.Second),
	}

	log.Printf("starting simulator with %d devices", cfg.Count)

	var wg sync.WaitGroup
	for index := 0; index < cfg.Count; index++ {
		wg.Add(1)
		deviceID := fmt.Sprintf("%s-%03d", cfg.DevicePrefix, index+1)
		go func(id string) {
			defer wg.Done()
			runDevice(ctx, cfg, id)
		}(deviceID)
	}

	<-ctx.Done()
	wg.Wait()
}

func runDevice(ctx context.Context, cfg simulatorConfig, deviceID string) {
	backoff := time.Second

	for ctx.Err() == nil {
		token, err := ensureToken(ctx, cfg, deviceID)
		if err != nil {
			log.Printf("[%s] bootstrap failed: %v", deviceID, err)
			if !sleepContext(ctx, backoff) {
				return
			}
			continue
		}

		client, err := qttclient.Dial(ctx, qttclient.Config{
			Addr:     cfg.BrokerAddr,
			ClientID: deviceID,
			Token:    token,
		})
		if err != nil {
			log.Printf("[%s] dial failed: %v", deviceID, err)
			if !sleepContext(ctx, backoff) {
				return
			}
			continue
		}

		if _, err := client.Connect(ctx, map[string]string{"kind": "simulator"}); err != nil {
			_ = client.Close()
			log.Printf("[%s] connect failed: %v", deviceID, err)
			if !sleepContext(ctx, backoff) {
				return
			}
			continue
		}

		if err := client.Subscribe(ctx, []qtttypes.Subscription{{Topic: fmt.Sprintf("device/%s/command", deviceID), QoS: 1}}); err != nil {
			_ = client.Close()
			log.Printf("[%s] subscribe failed: %v", deviceID, err)
			if !sleepContext(ctx, backoff) {
				return
			}
			continue
		}

		if _, err := client.ReadPacket(ctx); err != nil {
			_ = client.Close()
			log.Printf("[%s] suback failed: %v", deviceID, err)
			if !sleepContext(ctx, backoff) {
				return
			}
			continue
		}

		if err := runTelemetryLoop(ctx, client, cfg, deviceID); err != nil {
			_ = client.Close()
			log.Printf("[%s] connection lost: %v", deviceID, err)
			if !sleepContext(ctx, backoff) {
				return
			}
			continue
		}
	}
}

func runTelemetryLoop(ctx context.Context, client *qttclient.Client, cfg simulatorConfig, deviceID string) error {
	errCh := make(chan error, 1)
	go func() {
		errCh <- readCommands(ctx, client, deviceID)
	}()

	ticker := time.NewTicker(cfg.Interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case err := <-errCh:
			return err
		case timestamp := <-ticker.C:
			payload, _ := json.Marshal(map[string]any{
				"temperature": 18 + rand.Float64()*10,
				"humidity":    35 + rand.Float64()*25,
				"battery":     40 + rand.Intn(60),
				"signal":      -75 + rand.Intn(25),
				"status":      "online",
				"sampledAt":   timestamp.UTC().Format(time.RFC3339),
			})
			if err := client.Publish(ctx, fmt.Sprintf("device/%s/telemetry", deviceID), 1, payload); err != nil {
				return err
			}
		}
	}
}

func readCommands(ctx context.Context, client *qttclient.Client, deviceID string) error {
	for {
		packet, err := client.ReadPacket(ctx)
		if err != nil {
			return err
		}

		if packet.Type == qtttypes.PacketPublish {
			log.Printf("[%s] received command on %s: %s", deviceID, packet.Topic, string(packet.Payload))
			if packet.QoS > 0 {
				if err := client.SendPubAck(ctx, packet.PacketID); err != nil {
					return err
				}
			}
		}
	}
}

func ensureToken(ctx context.Context, cfg simulatorConfig, deviceID string) (string, error) {
	registerBody, _ := json.Marshal(map[string]any{
		"id":   deviceID,
		"name": deviceID,
		"metadata": map[string]any{
			"source": "simulator",
		},
	})
	if err := doJSON(ctx, http.MethodPost, fmt.Sprintf("%s/devices/register", cfg.APIBaseURL), registerBody, nil); err != nil {
		return "", err
	}

	var response struct {
		Token string `json:"token"`
	}
	if err := doJSON(ctx, http.MethodPost, fmt.Sprintf("%s/devices/%s/token", cfg.APIBaseURL, deviceID), nil, &response); err != nil {
		return "", err
	}

	return response.Token, nil
}

func doJSON(ctx context.Context, method string, url string, payload []byte, out any) error {
	var body io.Reader
	if payload != nil {
		body = bytes.NewReader(payload)
	}

	request, err := http.NewRequestWithContext(ctx, method, url, body)
	if err != nil {
		return err
	}
	if payload != nil {
		request.Header.Set("Content-Type", "application/json")
	}

	response, err := http.DefaultClient.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()

	responseBody, err := io.ReadAll(response.Body)
	if err != nil {
		return err
	}
	if response.StatusCode >= 300 {
		return fmt.Errorf("%s returned %s: %s", url, response.Status, string(responseBody))
	}
	if out != nil {
		if err := json.Unmarshal(responseBody, out); err != nil {
			return err
		}
	}
	return nil
}

func env(key string, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}

func envInt(key string, fallback int) int {
	value := os.Getenv(key)
	if value == "" {
		return fallback
	}

	var parsed int
	if _, err := fmt.Sscanf(value, "%d", &parsed); err != nil {
		return fallback
	}
	return parsed
}

func envDuration(key string, fallback time.Duration) time.Duration {
	value := os.Getenv(key)
	if value == "" {
		return fallback
	}

	parsed, err := time.ParseDuration(value)
	if err != nil {
		return fallback
	}
	return parsed
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
