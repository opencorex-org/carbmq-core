package subscription

import (
	"sync"

	qtttypes "github.com/opencorex/crabmq-core/pkg/qtt/types"
	"github.com/opencorex/crabmq-core/pkg/qtt/utils"
)

type Match struct {
	ClientID string
	Filter   string
	QoS      int
}

type Registry struct {
	mu       sync.RWMutex
	byClient map[string][]qtttypes.Subscription
}

func NewRegistry() *Registry {
	return &Registry{
		byClient: make(map[string][]qtttypes.Subscription),
	}
}

func (r *Registry) Add(clientID string, subscriptions []qtttypes.Subscription) {
	r.mu.Lock()
	defer r.mu.Unlock()

	existing := r.byClient[clientID]
	for _, next := range subscriptions {
		replaced := false
		for idx := range existing {
			if existing[idx].Topic == next.Topic {
				existing[idx] = next
				replaced = true
				break
			}
		}
		if !replaced {
			existing = append(existing, next)
		}
	}

	r.byClient[clientID] = existing
}

func (r *Registry) List(clientID string) []qtttypes.Subscription {
	r.mu.RLock()
	defer r.mu.RUnlock()

	values := r.byClient[clientID]
	return append([]qtttypes.Subscription(nil), values...)
}

func (r *Registry) Match(topic string) []Match {
	r.mu.RLock()
	defer r.mu.RUnlock()

	matches := make([]Match, 0)
	for clientID, subscriptions := range r.byClient {
		for _, subscription := range subscriptions {
			if utils.MatchTopic(subscription.Topic, topic) {
				matches = append(matches, Match{
					ClientID: clientID,
					Filter:   subscription.Topic,
					QoS:      subscription.QoS,
				})
			}
		}
	}

	return matches
}
