package server

import (
	"sync"
	"time"
)

type rateLimiter struct {
	limit  int
	window time.Duration
	mu     sync.Mutex
	seen   map[string]*rateWindow
}

type rateWindow struct {
	resetAt time.Time
	count   int
}

func newRateLimiter(limit int, window time.Duration) *rateLimiter {
	return &rateLimiter{
		limit:  limit,
		window: window,
		seen:   make(map[string]*rateWindow),
	}
}

func (r *rateLimiter) Allow(clientID string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()

	now := time.Now().UTC()
	entry, ok := r.seen[clientID]
	if !ok || now.After(entry.resetAt) {
		r.seen[clientID] = &rateWindow{
			resetAt: now.Add(r.window),
			count:   1,
		}
		return true
	}

	if entry.count >= r.limit {
		return false
	}

	entry.count++
	return true
}
