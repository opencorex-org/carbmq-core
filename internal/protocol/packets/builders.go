package packets

import (
	"github.com/opencorex/crabmq-core/pkg/qtt/types"
	"github.com/opencorex/crabmq-core/pkg/qtt/utils"
)

func ConnAck(status string, sessionPresent bool) types.Packet {
	return types.Packet{
		Type:           types.PacketConnAck,
		Status:         status,
		SessionPresent: sessionPresent,
		Timestamp:      utils.TimestampNow(),
	}
}

func SubAck(packetID string) types.Packet {
	return types.Packet{
		Type:      types.PacketSubAck,
		PacketID:  packetID,
		Status:    "subscribed",
		Timestamp: utils.TimestampNow(),
	}
}

func PubAck(packetID string) types.Packet {
	return types.Packet{
		Type:      types.PacketPubAck,
		PacketID:  packetID,
		Status:    "acknowledged",
		Timestamp: utils.TimestampNow(),
	}
}

func Pong() types.Packet {
	return types.Packet{
		Type:      types.PacketPong,
		Status:    "ok",
		Timestamp: utils.TimestampNow(),
	}
}

func Error(code string, message string) types.Packet {
	return types.Packet{
		Type:      types.PacketError,
		Code:      code,
		Error:     message,
		Timestamp: utils.TimestampNow(),
	}
}
