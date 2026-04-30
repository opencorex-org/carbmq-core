package connection

import (
	"context"

	qtttypes "github.com/opencorex/crabmq-core/pkg/qtt/types"
)

type PacketConn interface {
	ID() string
	RemoteAddr() string
	Send(context.Context, qtttypes.Packet) error
	Receive(context.Context) (qtttypes.Packet, error)
	Close() error
}

type Handler func(context.Context, PacketConn)
