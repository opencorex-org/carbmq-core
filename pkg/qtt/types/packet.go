package types

import (
	"encoding/json"
	"time"
)

type PacketType string

const (
	PacketConnect    PacketType = "CONNECT"
	PacketConnAck    PacketType = "CONNACK"
	PacketPublish    PacketType = "PUBLISH"
	PacketPubAck     PacketType = "PUBACK"
	PacketSubscribe  PacketType = "SUBSCRIBE"
	PacketSubAck     PacketType = "SUBACK"
	PacketPing       PacketType = "PING"
	PacketPong       PacketType = "PONG"
	PacketDisconnect PacketType = "DISCONNECT"
	PacketError      PacketType = "ERROR"
)

type Subscription struct {
	Topic string `json:"topic"`
	QoS   int    `json:"qos"`
}

type Packet struct {
	Type           PacketType        `json:"type"`
	PacketID       string            `json:"packetId,omitempty"`
	ClientID       string            `json:"clientId,omitempty"`
	Token          string            `json:"token,omitempty"`
	Topic          string            `json:"topic,omitempty"`
	QoS            int               `json:"qos,omitempty"`
	Payload        json.RawMessage   `json:"payload,omitempty"`
	Timestamp      time.Time         `json:"timestamp"`
	Subscriptions  []Subscription    `json:"subscriptions,omitempty"`
	SessionPresent bool              `json:"sessionPresent,omitempty"`
	Status         string            `json:"status,omitempty"`
	Code           string            `json:"code,omitempty"`
	Error          string            `json:"error,omitempty"`
	Metadata       map[string]string `json:"metadata,omitempty"`
}

func (p Packet) Clone() Packet {
	cloned := p
	if p.Payload != nil {
		cloned.Payload = append(json.RawMessage(nil), p.Payload...)
	}
	if len(p.Subscriptions) > 0 {
		cloned.Subscriptions = append([]Subscription(nil), p.Subscriptions...)
	}
	if len(p.Metadata) > 0 {
		cloned.Metadata = make(map[string]string, len(p.Metadata))
		for key, value := range p.Metadata {
			cloned.Metadata[key] = value
		}
	}

	return cloned
}
