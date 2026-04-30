package memory

import (
	"sync"

	qtttypes "github.com/opencorex/crabmq-core/pkg/qtt/types"
)

type Store struct {
	mu    sync.Mutex
	items map[string][]qtttypes.Packet
}

func NewStore() *Store {
	return &Store{
		items: make(map[string][]qtttypes.Packet),
	}
}

func (s *Store) Enqueue(clientID string, packet qtttypes.Packet) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.items[clientID] = append(s.items[clientID], packet.Clone())
}

func (s *Store) Drain(clientID string) []qtttypes.Packet {
	s.mu.Lock()
	defer s.mu.Unlock()

	values := append([]qtttypes.Packet(nil), s.items[clientID]...)
	delete(s.items, clientID)
	return values
}
