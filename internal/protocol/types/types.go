package types

import qtttypes "github.com/opencorex/crabmq-core/pkg/qtt/types"

type Packet = qtttypes.Packet
type PacketType = qtttypes.PacketType
type Subscription = qtttypes.Subscription

const (
	PacketConnect    = qtttypes.PacketConnect
	PacketConnAck    = qtttypes.PacketConnAck
	PacketPublish    = qtttypes.PacketPublish
	PacketPubAck     = qtttypes.PacketPubAck
	PacketSubscribe  = qtttypes.PacketSubscribe
	PacketSubAck     = qtttypes.PacketSubAck
	PacketPing       = qtttypes.PacketPing
	PacketPong       = qtttypes.PacketPong
	PacketDisconnect = qtttypes.PacketDisconnect
	PacketError      = qtttypes.PacketError
)
