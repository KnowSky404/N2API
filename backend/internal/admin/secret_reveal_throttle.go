package admin

import (
	"container/list"
	"strconv"
	"sync"
	"time"
)

type SecretRevealThrottleConfig struct {
	Limit      int
	Window     time.Duration
	MaxEntries int
}

type SecretRevealThrottleDecision struct {
	Allowed    bool
	RetryAfter time.Duration
}

type SecretRevealThrottle struct {
	mu      sync.Mutex
	config  SecretRevealThrottleConfig
	entries map[string]*secretRevealThrottleEntry
	order   list.List
	now     func() time.Time
}

type secretRevealThrottleEntry struct {
	key     string
	count   int
	resetAt time.Time
	element *list.Element
}

func NewSecretRevealThrottle(config SecretRevealThrottleConfig) *SecretRevealThrottle {
	return &SecretRevealThrottle{
		config:  config,
		entries: make(map[string]*secretRevealThrottleEntry),
		now:     time.Now,
	}
}

// BeginAttempt atomically reserves one attempt across every reveal identity.
func (t *SecretRevealThrottle) BeginAttempt(ip string, adminID, keyID int64) SecretRevealThrottleDecision {
	if t == nil || t.config.Limit <= 0 || t.config.Window <= 0 || t.config.MaxEntries <= 0 {
		return SecretRevealThrottleDecision{Allowed: true}
	}

	now := t.now()
	keys := secretRevealThrottleKeys(ip, adminID, keyID)
	t.mu.Lock()
	defer t.mu.Unlock()
	t.purgeExpired(now)

	decision := SecretRevealThrottleDecision{Allowed: true}
	for _, key := range keys {
		entry := t.entries[key]
		if entry == nil || entry.count < t.config.Limit {
			continue
		}
		decision.Allowed = false
		if remaining := entry.resetAt.Sub(now); remaining > decision.RetryAfter {
			decision.RetryAfter = remaining
		}
	}
	if !decision.Allowed {
		return decision
	}

	missing := 0
	for _, key := range keys {
		if t.entries[key] == nil {
			missing++
		}
	}
	if len(t.entries)+missing > t.config.MaxEntries {
		return SecretRevealThrottleDecision{RetryAfter: t.config.Window}
	}

	for _, key := range keys {
		entry := t.entries[key]
		if entry == nil {
			entry = &secretRevealThrottleEntry{key: key, resetAt: now.Add(t.config.Window)}
			entry.element = t.order.PushBack(entry)
			t.entries[key] = entry
		}
		entry.count++
	}
	return decision
}

func secretRevealThrottleKeys(ip string, adminID, keyID int64) []string {
	keys := make([]string, 0, 3)
	if normalized := normalizeLoginThrottleIP(ip); normalized != "" {
		keys = append(keys, "ip:"+normalized)
	}
	keys = append(keys,
		"admin:"+strconv.FormatInt(adminID, 10),
		"key:"+strconv.FormatInt(keyID, 10),
	)
	return keys
}

func (t *SecretRevealThrottle) purgeExpired(now time.Time) {
	for {
		front := t.order.Front()
		if front == nil {
			return
		}
		entry := front.Value.(*secretRevealThrottleEntry)
		if now.Before(entry.resetAt) {
			return
		}
		delete(t.entries, entry.key)
		t.order.Remove(front)
	}
}
