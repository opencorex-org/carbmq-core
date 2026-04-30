package session

import (
	"sync"
	"time"

	authjwt "github.com/opencorex/crabmq-core/internal/auth/jwt"
	"github.com/opencorex/crabmq-core/internal/transport/connection"
	qtttypes "github.com/opencorex/crabmq-core/pkg/qtt/types"
)

type Session struct {
	ClientID      string
	DeviceID      string
	Role          string
	Claims        authjwt.Claims
	Conn          connection.PacketConn
	Online        bool
	Subscriptions []qtttypes.Subscription
	ConnectedAt   time.Time
	LastSeenAt    time.Time
}

type Store struct {
	mu       sync.RWMutex
	sessions map[string]*Session
}

func NewStore() *Store {
	return &Store{
		sessions: make(map[string]*Session),
	}
}

func (s *Store) UpsertConnected(next Session) (bool, []qtttypes.Subscription) {
	s.mu.Lock()
	defer s.mu.Unlock()

	existing, ok := s.sessions[next.ClientID]
	if ok {
		next.Subscriptions = append([]qtttypes.Subscription(nil), existing.Subscriptions...)
	}

	next.Online = true
	if next.ConnectedAt.IsZero() {
		next.ConnectedAt = time.Now().UTC()
	}
	next.LastSeenAt = time.Now().UTC()

	s.sessions[next.ClientID] = &next
	return ok, append([]qtttypes.Subscription(nil), next.Subscriptions...)
}

func (s *Store) AddSubscriptions(clientID string, subscriptions []qtttypes.Subscription) {
	s.mu.Lock()
	defer s.mu.Unlock()

	session, ok := s.sessions[clientID]
	if !ok {
		return
	}

	for _, next := range subscriptions {
		replaced := false
		for idx := range session.Subscriptions {
			if session.Subscriptions[idx].Topic == next.Topic {
				session.Subscriptions[idx] = next
				replaced = true
				break
			}
		}
		if !replaced {
			session.Subscriptions = append(session.Subscriptions, next)
		}
	}
}

func (s *Store) Touch(clientID string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if session, ok := s.sessions[clientID]; ok {
		session.LastSeenAt = time.Now().UTC()
	}
}

func (s *Store) Disconnect(clientID string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if session, ok := s.sessions[clientID]; ok {
		session.Online = false
		session.Conn = nil
		session.LastSeenAt = time.Now().UTC()
	}
}

func (s *Store) Get(clientID string) (Session, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	session, ok := s.sessions[clientID]
	if !ok {
		return Session{}, false
	}

	copy := *session
	copy.Subscriptions = append([]qtttypes.Subscription(nil), session.Subscriptions...)
	return copy, true
}

func (s *Store) OnlineCount() int {
	s.mu.RLock()
	defer s.mu.RUnlock()

	total := 0
	for _, session := range s.sessions {
		if session.Online {
			total++
		}
	}

	return total
}
