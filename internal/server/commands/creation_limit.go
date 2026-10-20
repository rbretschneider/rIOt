package commands

import (
	"errors"
	"sync"
	"time"
)

var ErrDeviceNotFound = errors.New("device not found")
var ErrInvalidCommandParams = errors.New("invalid command parameters")
var ErrCreationRateLimited = errors.New("command creation rate limited")

type creationBucket struct {
	tokens   float64
	at, last time.Time
	policies map[string]time.Time
}
type creationGate struct {
	mu      sync.Mutex
	buckets map[string]*creationBucket
}

func (g *creationGate) allow(device, origin, target string, now time.Time) bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.buckets == nil {
		g.buckets = map[string]*creationBucket{}
	}
	for k, b := range g.buckets {
		for policy, t := range b.policies {
			if now.Sub(t) >= 15*time.Minute {
				delete(b.policies, policy)
			}
		}
		if now.Sub(b.last) > 10*time.Minute && len(b.policies) == 0 {
			delete(g.buckets, k)
		}
	}
	rate, capacity := 60.0/3600, 120.0
	if origin != "user" {
		rate = 30.0 / 3600
		capacity = 30
	}
	key := device + "/" + origin
	b := g.buckets[key]
	if b == nil {
		b = &creationBucket{tokens: capacity, at: now, policies: map[string]time.Time{}}
		g.buckets[key] = b
	}
	b.last = now
	b.tokens += now.Sub(b.at).Seconds() * rate
	if b.tokens > capacity {
		b.tokens = capacity
	}
	b.at = now
	if b.tokens < 1 {
		return false
	}
	if origin != "user" && target != "" {
		for k, t := range b.policies {
			if now.Sub(t) >= 15*time.Minute {
				delete(b.policies, k)
			}
		}
		if t, ok := b.policies[target]; ok && now.Sub(t) < 15*time.Minute {
			return false
		}
		if len(b.policies) >= 200 {
			return false
		}
		b.policies[target] = now
	}
	b.tokens--
	return true
}
