package client

import (
	"context"
	"crypto/tls"
	"fmt"
	"sync"
	"time"

	quicgo "github.com/quic-go/quic-go"

	"github.com/opencorex/crabmq-core/internal/protocol/decoder"
	"github.com/opencorex/crabmq-core/internal/protocol/encoder"
	qtttypes "github.com/opencorex/crabmq-core/pkg/qtt/types"
	"github.com/opencorex/crabmq-core/pkg/qtt/utils"
)

type Config struct {
	Addr         string
	ClientID     string
	Token        string
	ProtocolName string
	WriteTimeout time.Duration
	ReadTimeout  time.Duration
}

type Client struct {
	cfg     Config
	conn    quicgo.Connection
	stream  quicgo.Stream
	encoder *encoder.JSONEncoder
	decoder *decoder.JSONDecoder
	mu      sync.Mutex
}

func Dial(ctx context.Context, cfg Config) (*Client, error) {
	if cfg.ProtocolName == "" {
		cfg.ProtocolName = "crabmq-qtt"
	}
	if cfg.WriteTimeout <= 0 {
		cfg.WriteTimeout = 5 * time.Second
	}
	if cfg.ReadTimeout <= 0 {
		cfg.ReadTimeout = 45 * time.Second
	}

	conn, err := quicgo.DialAddr(
		ctx,
		cfg.Addr,
		&tls.Config{
			InsecureSkipVerify: true,
			NextProtos:         []string{cfg.ProtocolName},
			MinVersion:         tls.VersionTLS13,
		},
		&quicgo.Config{
			KeepAlivePeriod: 20 * time.Second,
			MaxIdleTimeout:  cfg.ReadTimeout,
		},
	)
	if err != nil {
		return nil, fmt.Errorf("dial quic: %w", err)
	}

	stream, err := conn.OpenStreamSync(ctx)
	if err != nil {
		_ = conn.CloseWithError(1, "stream open failed")
		return nil, fmt.Errorf("open stream: %w", err)
	}

	return &Client{
		cfg:     cfg,
		conn:    conn,
		stream:  stream,
		encoder: encoder.NewJSONEncoder(stream),
		decoder: decoder.NewJSONDecoder(stream),
	}, nil
}

func (c *Client) Connect(ctx context.Context, metadata map[string]string) (qtttypes.Packet, error) {
	connectPacket := qtttypes.Packet{
		Type:      qtttypes.PacketConnect,
		ClientID:  c.cfg.ClientID,
		Token:     c.cfg.Token,
		Metadata:  metadata,
		Timestamp: utils.TimestampNow(),
	}

	if err := c.Send(ctx, connectPacket); err != nil {
		return qtttypes.Packet{}, err
	}

	packet, err := c.ReadPacket(ctx)
	if err != nil {
		return qtttypes.Packet{}, err
	}
	if packet.Type != qtttypes.PacketConnAck {
		return qtttypes.Packet{}, fmt.Errorf("expected CONNACK, got %s", packet.Type)
	}

	return packet, nil
}

func (c *Client) Publish(ctx context.Context, topic string, qos int, payload []byte) error {
	packet := qtttypes.Packet{
		Type:      qtttypes.PacketPublish,
		PacketID:  utils.NewPacketID(),
		ClientID:  c.cfg.ClientID,
		Topic:     topic,
		QoS:       qos,
		Payload:   payload,
		Timestamp: utils.TimestampNow(),
	}

	return c.Send(ctx, packet)
}

func (c *Client) Subscribe(ctx context.Context, subscriptions []qtttypes.Subscription) error {
	packet := qtttypes.Packet{
		Type:          qtttypes.PacketSubscribe,
		PacketID:      utils.NewPacketID(),
		ClientID:      c.cfg.ClientID,
		Subscriptions: subscriptions,
		Timestamp:     utils.TimestampNow(),
	}

	return c.Send(ctx, packet)
}

func (c *Client) SendPubAck(ctx context.Context, packetID string) error {
	return c.Send(ctx, qtttypes.Packet{
		Type:      qtttypes.PacketPubAck,
		PacketID:  packetID,
		ClientID:  c.cfg.ClientID,
		Timestamp: utils.TimestampNow(),
	})
}

func (c *Client) Ping(ctx context.Context) error {
	return c.Send(ctx, qtttypes.Packet{
		Type:      qtttypes.PacketPing,
		ClientID:  c.cfg.ClientID,
		Timestamp: utils.TimestampNow(),
	})
}

func (c *Client) Send(ctx context.Context, packet qtttypes.Packet) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if deadline, ok := ctx.Deadline(); ok {
		_ = c.stream.SetWriteDeadline(deadline)
		defer c.stream.SetWriteDeadline(time.Time{})
	} else if c.cfg.WriteTimeout > 0 {
		_ = c.stream.SetWriteDeadline(time.Now().Add(c.cfg.WriteTimeout))
		defer c.stream.SetWriteDeadline(time.Time{})
	}

	return c.encoder.Encode(packet)
}

func (c *Client) ReadPacket(ctx context.Context) (qtttypes.Packet, error) {
	if deadline, ok := ctx.Deadline(); ok {
		_ = c.stream.SetReadDeadline(deadline)
		defer c.stream.SetReadDeadline(time.Time{})
	} else if c.cfg.ReadTimeout > 0 {
		_ = c.stream.SetReadDeadline(time.Now().Add(c.cfg.ReadTimeout))
		defer c.stream.SetReadDeadline(time.Time{})
	}

	packet, err := c.decoder.Decode()
	if err != nil {
		return qtttypes.Packet{}, fmt.Errorf("read packet: %w", err)
	}

	return packet, nil
}

func (c *Client) Close() error {
	_ = c.stream.Close()
	return c.conn.CloseWithError(0, "closed")
}
