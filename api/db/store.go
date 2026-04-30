package db

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	_ "github.com/lib/pq"
)

type Device struct {
	ID        string         `json:"id"`
	Name      string         `json:"name"`
	Metadata  map[string]any `json:"metadata,omitempty"`
	CreatedAt time.Time      `json:"createdAt"`
}

type Message struct {
	PacketID     string          `json:"packetId"`
	ClientID     string          `json:"clientId,omitempty"`
	Topic        string          `json:"topic"`
	QoS          int             `json:"qos"`
	Payload      json.RawMessage `json:"payload"`
	PacketType   string          `json:"packetType"`
	Direction    string          `json:"direction"`
	Timestamp    time.Time       `json:"timestamp"`
	Acknowledged bool            `json:"acknowledged"`
}

type OfflineMessage struct {
	PacketID    string          `json:"packetId"`
	ClientID    string          `json:"clientId"`
	Topic       string          `json:"topic"`
	QoS         int             `json:"qos"`
	Payload     json.RawMessage `json:"payload"`
	EnqueuedAt  time.Time       `json:"enqueuedAt"`
	AvailableAt time.Time       `json:"availableAt"`
}

type MetricsSummary struct {
	DeviceCount        int `json:"deviceCount"`
	MessageCount       int `json:"messageCount"`
	OfflineQueueDepth  int `json:"offlineQueueDepth"`
	RecentMessageCount int `json:"recentMessageCount"`
}

type Store struct {
	db *sql.DB
}

func Open(ctx context.Context, dsn string) (*Store, error) {
	db, err := sql.Open("postgres", dsn)
	if err != nil {
		return nil, fmt.Errorf("open postgres: %w", err)
	}

	db.SetMaxOpenConns(25)
	db.SetMaxIdleConns(10)
	db.SetConnMaxLifetime(30 * time.Minute)

	if err := db.PingContext(ctx); err != nil {
		return nil, fmt.Errorf("ping postgres: %w", err)
	}

	store := &Store{db: db}
	if err := store.EnsureSchema(ctx); err != nil {
		_ = db.Close()
		return nil, err
	}

	return store, nil
}

func (s *Store) Close() error {
	return s.db.Close()
}

func (s *Store) EnsureSchema(ctx context.Context) error {
	const schema = `
CREATE TABLE IF NOT EXISTS devices (
	id TEXT PRIMARY KEY,
	name TEXT NOT NULL,
	metadata JSONB NOT NULL DEFAULT '{}'::jsonb,
	created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS messages (
	packet_id TEXT PRIMARY KEY,
	client_id TEXT NOT NULL DEFAULT '',
	topic TEXT NOT NULL,
	qos SMALLINT NOT NULL,
	payload JSONB NOT NULL DEFAULT '{}'::jsonb,
	packet_type TEXT NOT NULL,
	direction TEXT NOT NULL,
	timestamp TIMESTAMPTZ NOT NULL,
	acknowledged BOOLEAN NOT NULL DEFAULT FALSE
);

CREATE INDEX IF NOT EXISTS idx_messages_topic_timestamp
ON messages (topic, timestamp DESC);

CREATE TABLE IF NOT EXISTS offline_messages (
	packet_id TEXT PRIMARY KEY,
	client_id TEXT NOT NULL,
	topic TEXT NOT NULL,
	qos SMALLINT NOT NULL,
	payload JSONB NOT NULL DEFAULT '{}'::jsonb,
	enqueued_at TIMESTAMPTZ NOT NULL,
	available_at TIMESTAMPTZ NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_offline_messages_client
ON offline_messages (client_id, available_at ASC);
`

	if _, err := s.db.ExecContext(ctx, schema); err != nil {
		return fmt.Errorf("ensure schema: %w", err)
	}

	return nil
}

func (s *Store) RegisterDevice(ctx context.Context, id string, name string, metadata map[string]any) (Device, error) {
	if id == "" {
		return Device{}, errors.New("device id is required")
	}
	if name == "" {
		name = id
	}

	payload, err := json.Marshal(metadata)
	if err != nil {
		return Device{}, fmt.Errorf("marshal device metadata: %w", err)
	}

	const query = `
INSERT INTO devices (id, name, metadata)
VALUES ($1, $2, $3)
ON CONFLICT (id) DO UPDATE
SET name = EXCLUDED.name,
    metadata = EXCLUDED.metadata
RETURNING id, name, metadata, created_at;
`

	device, err := scanDevice(s.db.QueryRowContext(ctx, query, id, name, payload))
	if err != nil {
		return Device{}, err
	}

	return device, nil
}

func (s *Store) GetDevice(ctx context.Context, id string) (Device, error) {
	const query = `SELECT id, name, metadata, created_at FROM devices WHERE id = $1;`
	device, err := scanDevice(s.db.QueryRowContext(ctx, query, id))
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Device{}, fmt.Errorf("device %s not found", id)
		}
		return Device{}, err
	}

	return device, nil
}

func (s *Store) ListDevices(ctx context.Context) ([]Device, error) {
	const query = `SELECT id, name, metadata, created_at FROM devices ORDER BY created_at DESC;`
	rows, err := s.db.QueryContext(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("list devices: %w", err)
	}
	defer rows.Close()

	devices := make([]Device, 0)
	for rows.Next() {
		device, err := scanDevice(rows)
		if err != nil {
			return nil, err
		}
		devices = append(devices, device)
	}

	return devices, rows.Err()
}

func (s *Store) SaveMessage(ctx context.Context, message Message) error {
	if len(message.Payload) == 0 {
		message.Payload = json.RawMessage(`{}`)
	}

	const query = `
INSERT INTO messages (packet_id, client_id, topic, qos, payload, packet_type, direction, timestamp, acknowledged)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
ON CONFLICT (packet_id) DO NOTHING;
`

	_, err := s.db.ExecContext(
		ctx,
		query,
		message.PacketID,
		message.ClientID,
		message.Topic,
		message.QoS,
		[]byte(message.Payload),
		message.PacketType,
		message.Direction,
		message.Timestamp,
		message.Acknowledged,
	)
	if err != nil {
		return fmt.Errorf("save message: %w", err)
	}

	return nil
}

func (s *Store) MarkMessageAcknowledged(ctx context.Context, packetID string) error {
	const query = `UPDATE messages SET acknowledged = TRUE WHERE packet_id = $1;`
	if _, err := s.db.ExecContext(ctx, query, packetID); err != nil {
		return fmt.Errorf("mark acknowledged: %w", err)
	}

	return nil
}

func (s *Store) GetTelemetry(ctx context.Context, deviceID string, limit int) ([]Message, error) {
	if limit <= 0 {
		limit = 50
	}

	const query = `
SELECT packet_id, client_id, topic, qos, payload, packet_type, direction, timestamp, acknowledged
FROM messages
WHERE topic LIKE $1
ORDER BY timestamp DESC
LIMIT $2;
`

	return s.queryMessages(ctx, query, fmt.Sprintf("device/%s/telemetry%%", deviceID), limit)
}

func (s *Store) ListMessages(ctx context.Context, limit int) ([]Message, error) {
	if limit <= 0 {
		limit = 100
	}

	const query = `
SELECT packet_id, client_id, topic, qos, payload, packet_type, direction, timestamp, acknowledged
FROM messages
ORDER BY timestamp DESC
LIMIT $1;
`

	return s.queryMessages(ctx, query, limit)
}

func (s *Store) EnqueueOfflineMessage(ctx context.Context, message OfflineMessage) error {
	if len(message.Payload) == 0 {
		message.Payload = json.RawMessage(`{}`)
	}

	const query = `
INSERT INTO offline_messages (packet_id, client_id, topic, qos, payload, enqueued_at, available_at)
VALUES ($1, $2, $3, $4, $5, $6, $7)
ON CONFLICT (packet_id) DO NOTHING;
`

	_, err := s.db.ExecContext(
		ctx,
		query,
		message.PacketID,
		message.ClientID,
		message.Topic,
		message.QoS,
		[]byte(message.Payload),
		message.EnqueuedAt,
		message.AvailableAt,
	)
	if err != nil {
		return fmt.Errorf("enqueue offline message: %w", err)
	}

	return nil
}

func (s *Store) GetOfflineMessages(ctx context.Context, clientID string) ([]OfflineMessage, error) {
	const query = `
SELECT packet_id, client_id, topic, qos, payload, enqueued_at, available_at
FROM offline_messages
WHERE client_id = $1
ORDER BY available_at ASC;
`

	rows, err := s.db.QueryContext(ctx, query, clientID)
	if err != nil {
		return nil, fmt.Errorf("get offline messages: %w", err)
	}
	defer rows.Close()

	messages := make([]OfflineMessage, 0)
	for rows.Next() {
		var message OfflineMessage
		var payload []byte
		if err := rows.Scan(
			&message.PacketID,
			&message.ClientID,
			&message.Topic,
			&message.QoS,
			&payload,
			&message.EnqueuedAt,
			&message.AvailableAt,
		); err != nil {
			return nil, fmt.Errorf("scan offline message: %w", err)
		}
		message.Payload = append(json.RawMessage(nil), payload...)
		messages = append(messages, message)
	}

	return messages, rows.Err()
}

func (s *Store) DeleteOfflineMessage(ctx context.Context, packetID string) error {
	const query = `DELETE FROM offline_messages WHERE packet_id = $1;`
	if _, err := s.db.ExecContext(ctx, query, packetID); err != nil {
		return fmt.Errorf("delete offline message: %w", err)
	}

	return nil
}

func (s *Store) GetMetricsSummary(ctx context.Context) (MetricsSummary, error) {
	summary := MetricsSummary{}

	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM devices;`).Scan(&summary.DeviceCount); err != nil {
		return MetricsSummary{}, fmt.Errorf("count devices: %w", err)
	}
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM messages;`).Scan(&summary.MessageCount); err != nil {
		return MetricsSummary{}, fmt.Errorf("count messages: %w", err)
	}
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM offline_messages;`).Scan(&summary.OfflineQueueDepth); err != nil {
		return MetricsSummary{}, fmt.Errorf("count offline messages: %w", err)
	}
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM messages WHERE timestamp >= NOW() - INTERVAL '1 hour';`).Scan(&summary.RecentMessageCount); err != nil {
		return MetricsSummary{}, fmt.Errorf("count recent messages: %w", err)
	}

	return summary, nil
}

func (s *Store) queryMessages(ctx context.Context, query string, args ...any) ([]Message, error) {
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("query messages: %w", err)
	}
	defer rows.Close()

	messages := make([]Message, 0)
	for rows.Next() {
		var message Message
		var payload []byte
		if err := rows.Scan(
			&message.PacketID,
			&message.ClientID,
			&message.Topic,
			&message.QoS,
			&payload,
			&message.PacketType,
			&message.Direction,
			&message.Timestamp,
			&message.Acknowledged,
		); err != nil {
			return nil, fmt.Errorf("scan message: %w", err)
		}
		message.Payload = append(json.RawMessage(nil), payload...)
		messages = append(messages, message)
	}

	return messages, rows.Err()
}

type rowScanner interface {
	Scan(dest ...any) error
}

func scanDevice(scanner rowScanner) (Device, error) {
	var device Device
	var raw []byte
	if err := scanner.Scan(&device.ID, &device.Name, &raw, &device.CreatedAt); err != nil {
		return Device{}, err
	}

	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &device.Metadata); err != nil {
			return Device{}, fmt.Errorf("unmarshal device metadata: %w", err)
		}
	}

	return device, nil
}
