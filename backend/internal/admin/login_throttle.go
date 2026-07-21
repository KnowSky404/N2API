package admin

import (
	"container/list"
	"crypto/sha256"
	"encoding/hex"
	"net/netip"
	"strings"
	"sync"
	"time"
)

type LoginThrottleConfig struct {
	FailureThreshold int
	BaseDelay        time.Duration
	MaxDelay         time.Duration
	EntryTTL         time.Duration
	EventWindow      time.Duration
	MaxEntries       int
}

type LoginThrottleDecision struct {
	Allowed     bool
	RetryAfter  time.Duration
	ReportEvent bool
}

type LoginThrottleAttempt struct {
	throttle *LoginThrottle
	keys     []string
	active   bool
}

type LoginThrottle struct {
	mu        sync.Mutex
	config    LoginThrottleConfig
	entries   map[string]*loginThrottleEntry
	lru       list.List
	now       func() time.Time
	lastEvent time.Time
}

type loginThrottleEntry struct {
	key          string
	failures     int
	inFlight     int
	blockedUntil time.Time
	lastSeen     time.Time
	element      *list.Element
}

func NewLoginThrottle(config LoginThrottleConfig) *LoginThrottle {
	return &LoginThrottle{
		config:  config,
		entries: make(map[string]*loginThrottleEntry),
		now:     time.Now,
	}
}

// BeginAttempt atomically checks both identities and reserves password verification capacity.
func (t *LoginThrottle) BeginAttempt(ip, username string) (*LoginThrottleAttempt, LoginThrottleDecision) {
	attempt := &LoginThrottleAttempt{throttle: t}
	if !t.enabled() {
		return attempt, allowedLoginThrottleDecision()
	}

	now := t.now()
	keys := loginThrottleKeys(ip, username)
	t.mu.Lock()
	defer t.mu.Unlock()
	t.purgeExpired(now)

	decision := allowedLoginThrottleDecision()
	for _, key := range keys {
		entry, ok := t.entries[key]
		if !ok {
			continue
		}
		t.touch(entry, now)
		if remaining := entry.blockedUntil.Sub(now); remaining > 0 {
			decision.Allowed = false
			if remaining > decision.RetryAfter {
				decision.RetryAfter = remaining
			}
		}
		if entry.inFlight > 0 && entry.failures+entry.inFlight >= t.config.FailureThreshold {
			decision.Allowed = false
			if t.config.BaseDelay > decision.RetryAfter {
				decision.RetryAfter = t.config.BaseDelay
			}
		}
	}
	if !decision.Allowed {
		decision.ReportEvent = t.shouldReportEvent(now)
		return attempt, decision
	}

	newEntries := 0
	for _, key := range keys {
		if _, ok := t.entries[key]; !ok {
			newEntries++
		}
	}
	if !t.ensureCapacity(newEntries, now) {
		return attempt, LoginThrottleDecision{
			RetryAfter:  max(t.config.BaseDelay, time.Second),
			ReportEvent: t.shouldReportEvent(now),
		}
	}

	for _, key := range keys {
		entry := t.entries[key]
		if entry == nil {
			entry = &loginThrottleEntry{key: key, lastSeen: now}
			entry.element = t.lru.PushBack(entry)
			t.entries[key] = entry
		}
		entry.inFlight++
		t.touch(entry, now)
	}
	attempt.keys = keys
	attempt.active = true
	return attempt, decision
}

func (t *LoginThrottle) RecordFailure(attempt *LoginThrottleAttempt) LoginThrottleDecision {
	if !t.enabled() || attempt == nil || attempt.throttle != t {
		return allowedLoginThrottleDecision()
	}

	now := t.now()
	t.mu.Lock()
	defer t.mu.Unlock()
	if !attempt.active {
		return allowedLoginThrottleDecision()
	}
	attempt.active = false

	decision := allowedLoginThrottleDecision()
	for _, key := range attempt.keys {
		entry := t.entries[key]
		if entry == nil || entry.inFlight <= 0 {
			continue
		}
		entry.inFlight--
		entry.failures++
		delay := t.failureDelay(entry.failures)
		if delay > 0 {
			entry.blockedUntil = now.Add(delay)
			decision.Allowed = false
			if delay > decision.RetryAfter {
				decision.RetryAfter = delay
			}
		}
		t.touch(entry, now)
	}
	decision.ReportEvent = t.shouldReportEvent(now)
	return decision
}

func (t *LoginThrottle) RecordSuccess(attempt *LoginThrottleAttempt) {
	t.finishAttempt(attempt, true)
}

func (t *LoginThrottle) CancelAttempt(attempt *LoginThrottleAttempt) {
	t.finishAttempt(attempt, false)
}

func (t *LoginThrottle) finishAttempt(attempt *LoginThrottleAttempt, success bool) {
	if !t.enabled() || attempt == nil || attempt.throttle != t {
		return
	}

	now := t.now()
	t.mu.Lock()
	defer t.mu.Unlock()
	if !attempt.active {
		return
	}
	attempt.active = false
	for _, key := range attempt.keys {
		entry := t.entries[key]
		if entry == nil || entry.inFlight <= 0 {
			continue
		}
		entry.inFlight--
		if success {
			entry.failures = 0
			entry.blockedUntil = time.Time{}
		}
		if entry.inFlight == 0 && entry.failures == 0 {
			t.removeEntry(entry)
			continue
		}
		t.touch(entry, now)
	}
}

func (t *LoginThrottle) enabled() bool {
	return t != nil && t.config.FailureThreshold > 0 && t.config.MaxEntries > 0
}

func allowedLoginThrottleDecision() LoginThrottleDecision {
	return LoginThrottleDecision{Allowed: true}
}

func loginThrottleKeys(ip, username string) []string {
	keys := make([]string, 0, 2)
	if normalized := normalizeLoginThrottleIP(ip); normalized != "" {
		keys = append(keys, "ip:"+normalized)
	}
	normalizedUsername := strings.ToLower(strings.TrimSpace(username))
	digest := sha256.Sum256([]byte(normalizedUsername))
	keys = append(keys, "username:"+hex.EncodeToString(digest[:]))
	return keys
}

func normalizeLoginThrottleIP(raw string) string {
	raw = strings.TrimSpace(raw)
	if addr, err := netip.ParseAddr(raw); err == nil && addr.Zone() == "" {
		return addr.Unmap().String()
	}
	return raw
}

func (t *LoginThrottle) touch(entry *loginThrottleEntry, now time.Time) {
	entry.lastSeen = now
	t.lru.MoveToBack(entry.element)
}

func (t *LoginThrottle) purgeExpired(now time.Time) {
	if t.config.EntryTTL <= 0 {
		return
	}
	for {
		oldest := t.oldestEntry()
		if oldest == nil || oldest.inFlight > 0 || now.Before(oldest.blockedUntil) || now.Before(oldest.lastSeen.Add(t.config.EntryTTL)) {
			return
		}
		t.removeEntry(oldest)
	}
}

func (t *LoginThrottle) ensureCapacity(required int, now time.Time) bool {
	for len(t.entries)+required > t.config.MaxEntries {
		oldest := t.oldestEntry()
		if oldest == nil {
			return false
		}
		if oldest.inFlight > 0 || oldest.failures > 0 || now.Before(oldest.blockedUntil) {
			return false
		}
		t.removeEntry(oldest)
	}
	return true
}

func (t *LoginThrottle) oldestEntry() *loginThrottleEntry {
	front := t.lru.Front()
	if front == nil {
		return nil
	}
	return front.Value.(*loginThrottleEntry)
}

func (t *LoginThrottle) removeEntry(entry *loginThrottleEntry) {
	delete(t.entries, entry.key)
	t.lru.Remove(entry.element)
}

func (t *LoginThrottle) failureDelay(failures int) time.Duration {
	if failures < t.config.FailureThreshold || t.config.BaseDelay <= 0 {
		return 0
	}
	maximum := t.config.MaxDelay
	if maximum <= 0 {
		maximum = t.config.BaseDelay
	}
	if t.config.BaseDelay >= maximum {
		return maximum
	}

	delay := t.config.BaseDelay
	for steps := failures - t.config.FailureThreshold; steps > 0; steps-- {
		if delay >= maximum || delay > maximum/2 {
			return maximum
		}
		delay *= 2
	}
	if delay > maximum {
		return maximum
	}
	return delay
}

func (t *LoginThrottle) shouldReportEvent(now time.Time) bool {
	if !t.lastEvent.IsZero() && t.config.EventWindow > 0 && now.Before(t.lastEvent.Add(t.config.EventWindow)) {
		return false
	}
	t.lastEvent = now
	return true
}
