package admin

import (
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"
)

type loginThrottleClock struct {
	mu  sync.Mutex
	now time.Time
}

func (c *loginThrottleClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *loginThrottleClock) Advance(duration time.Duration) {
	c.mu.Lock()
	c.now = c.now.Add(duration)
	c.mu.Unlock()
}

func newTestLoginThrottle(config LoginThrottleConfig) (*LoginThrottle, *loginThrottleClock) {
	clock := &loginThrottleClock{now: time.Date(2026, 7, 21, 12, 0, 0, 0, time.UTC)}
	throttle := NewLoginThrottle(config)
	throttle.now = clock.Now
	return throttle, clock
}

func TestLoginThrottleDisabledWhenThresholdIsNonPositive(t *testing.T) {
	for _, threshold := range []int{0, -1} {
		t.Run(fmt.Sprintf("threshold_%d", threshold), func(t *testing.T) {
			throttle, _ := newTestLoginThrottle(LoginThrottleConfig{
				FailureThreshold: threshold,
				BaseDelay:        time.Second,
				MaxDelay:         time.Minute,
				EntryTTL:         time.Hour,
				MaxEntries:       100,
			})
			for range 10 {
				attempt, begin := throttle.BeginAttempt("192.0.2.1", "Owner")
				assertThrottleDecision(t, begin, true, 0, false)
				assertThrottleDecision(t, throttle.RecordFailure(attempt), true, 0, false)
			}
			if len(throttle.entries) != 0 {
				t.Fatalf("entries = %d, want 0", len(throttle.entries))
			}
		})
	}
}

func TestLoginThrottleAppliesExponentialDelayWithMaximum(t *testing.T) {
	throttle, clock := newTestLoginThrottle(LoginThrottleConfig{
		FailureThreshold: 3,
		BaseDelay:        time.Second,
		MaxDelay:         4 * time.Second,
		EntryTTL:         time.Minute,
		EventWindow:      time.Minute,
		MaxEntries:       100,
	})

	assertThrottleDecision(t, recordLoginFailure(t, throttle, "192.0.2.1", "owner"), true, 0, true)
	assertThrottleDecision(t, recordLoginFailure(t, throttle, "192.0.2.1", "owner"), true, 0, false)
	assertThrottleDecision(t, recordLoginFailure(t, throttle, "192.0.2.1", "owner"), false, time.Second, false)
	assertThrottleDecision(t, inspectLoginThrottle(throttle, "192.0.2.1", "owner"), false, time.Second, false)

	clock.Advance(time.Second)
	assertThrottleDecision(t, inspectLoginThrottle(throttle, "192.0.2.1", "owner"), true, 0, false)
	assertThrottleDecision(t, recordLoginFailure(t, throttle, "192.0.2.1", "owner"), false, 2*time.Second, false)
	clock.Advance(2 * time.Second)
	assertThrottleDecision(t, recordLoginFailure(t, throttle, "192.0.2.1", "owner"), false, 4*time.Second, false)
	clock.Advance(4 * time.Second)
	assertThrottleDecision(t, recordLoginFailure(t, throttle, "192.0.2.1", "owner"), false, 4*time.Second, false)
}

func TestLoginThrottleAppliesIPAndUsernameIndependently(t *testing.T) {
	throttle, _ := newTestLoginThrottle(LoginThrottleConfig{
		FailureThreshold: 2,
		BaseDelay:        5 * time.Second,
		MaxDelay:         time.Minute,
		EntryTTL:         time.Hour,
		EventWindow:      time.Minute,
		MaxEntries:       100,
	})

	recordLoginFailure(t, throttle, "192.0.2.1", "first")
	assertThrottleDecision(t, recordLoginFailure(t, throttle, "192.0.2.1", "second"), false, 5*time.Second, false)
	assertThrottleDecision(t, inspectLoginThrottle(throttle, "192.0.2.1", "unseen"), false, 5*time.Second, false)
	assertThrottleDecision(t, inspectLoginThrottle(throttle, "192.0.2.2", "second"), true, 0, false)

	recordLoginFailure(t, throttle, "192.0.2.3", " Owner ")
	assertThrottleDecision(t, recordLoginFailure(t, throttle, "192.0.2.4", "OWNER"), false, 5*time.Second, false)
	assertThrottleDecision(t, inspectLoginThrottle(throttle, "192.0.2.5", "owner"), false, 5*time.Second, false)
	assertThrottleDecision(t, inspectLoginThrottle(throttle, "192.0.2.5", "other"), true, 0, false)
}

func TestLoginThrottleReturnsMaximumRetryAcrossDimensions(t *testing.T) {
	throttle, clock := newTestLoginThrottle(LoginThrottleConfig{
		FailureThreshold: 1,
		BaseDelay:        time.Second,
		MaxDelay:         8 * time.Second,
		EntryTTL:         time.Hour,
		EventWindow:      time.Minute,
		MaxEntries:       100,
	})

	recordLoginFailure(t, throttle, "192.0.2.1", "other-1")
	clock.Advance(time.Second)
	recordLoginFailure(t, throttle, "192.0.2.1", "other-2")
	clock.Advance(2 * time.Second)
	recordLoginFailure(t, throttle, "192.0.2.1", "owner")
	clock.Advance(time.Second)
	assertThrottleDecision(t, inspectLoginThrottle(throttle, "192.0.2.1", "owner"), false, 3*time.Second, false)
}

func TestLoginThrottleExpiresInactiveEntriesFromLRUFront(t *testing.T) {
	throttle, clock := newTestLoginThrottle(LoginThrottleConfig{
		FailureThreshold: 10,
		BaseDelay:        time.Second,
		MaxDelay:         time.Minute,
		EntryTTL:         10 * time.Second,
		EventWindow:      time.Minute,
		MaxEntries:       100,
	})

	recordLoginFailure(t, throttle, "192.0.2.1", "owner")
	clock.Advance(10 * time.Second)
	attempt, decision := throttle.BeginAttempt("192.0.2.2", "next")
	assertThrottleDecision(t, decision, true, 0, false)
	throttle.CancelAttempt(attempt)
	if len(throttle.entries) != 0 || throttle.lru.Len() != 0 {
		t.Fatalf("entries/list = %d/%d, want 0 after expiry and cancellation", len(throttle.entries), throttle.lru.Len())
	}
}

func TestLoginThrottleSuccessClearsBothIdentities(t *testing.T) {
	throttle, _ := newTestLoginThrottle(LoginThrottleConfig{
		FailureThreshold: 3,
		BaseDelay:        time.Minute,
		MaxDelay:         time.Minute,
		EntryTTL:         time.Hour,
		EventWindow:      time.Minute,
		MaxEntries:       100,
	})

	recordLoginFailure(t, throttle, "::ffff:192.0.2.1", " Owner ")
	attempt, decision := throttle.BeginAttempt("192.0.2.1", "OWNER")
	assertThrottleDecision(t, decision, true, 0, false)
	throttle.RecordSuccess(attempt)

	if len(throttle.entries) != 0 || throttle.lru.Len() != 0 {
		t.Fatalf("entries/list = %d/%d, want 0", len(throttle.entries), throttle.lru.Len())
	}
}

func TestLoginThrottleCapacityFailsClosedWithoutEvictingFailureState(t *testing.T) {
	throttle, clock := newTestLoginThrottle(LoginThrottleConfig{
		FailureThreshold: 10,
		BaseDelay:        time.Second,
		MaxDelay:         time.Minute,
		EntryTTL:         time.Hour,
		EventWindow:      time.Minute,
		MaxEntries:       3,
	})

	recordLoginFailure(t, throttle, "192.0.2.1", "first")
	recordLoginFailure(t, throttle, "192.0.2.1", "second")
	if len(throttle.entries) != 3 {
		t.Fatalf("entries = %d, want strict limit 3", len(throttle.entries))
	}

	attempt, decision := throttle.BeginAttempt("192.0.2.2", "third")
	assertThrottleDecision(t, decision, false, time.Second, false)
	throttle.CancelAttempt(attempt)
	for _, key := range loginThrottleKeys("192.0.2.1", "first") {
		if _, ok := throttle.entries[key]; !ok {
			t.Fatalf("protected failure entry %q was evicted", key)
		}
	}

	clock.Advance(time.Hour)
	attempt, decision = throttle.BeginAttempt("192.0.2.2", "third")
	assertThrottleDecision(t, decision, true, 0, false)
	throttle.CancelAttempt(attempt)
}

func TestLoginThrottleAggregatesEventsGloballyWithinWindow(t *testing.T) {
	throttle, clock := newTestLoginThrottle(LoginThrottleConfig{
		FailureThreshold: 10,
		BaseDelay:        time.Second,
		MaxDelay:         time.Minute,
		EntryTTL:         time.Hour,
		EventWindow:      10 * time.Second,
		MaxEntries:       100,
	})

	assertThrottleDecision(t, recordLoginFailure(t, throttle, "192.0.2.1", "owner-1"), true, 0, true)
	assertThrottleDecision(t, recordLoginFailure(t, throttle, "192.0.2.2", "owner-2"), true, 0, false)
	clock.Advance(10 * time.Second)
	assertThrottleDecision(t, recordLoginFailure(t, throttle, "192.0.2.3", "owner-3"), true, 0, true)
}

func TestLoginThrottleAtomicallyBoundsConcurrentAttempts(t *testing.T) {
	throttle, _ := newTestLoginThrottle(LoginThrottleConfig{
		FailureThreshold: 5,
		BaseDelay:        time.Second,
		MaxDelay:         time.Minute,
		EntryTTL:         time.Hour,
		EventWindow:      time.Minute,
		MaxEntries:       100,
	})

	type result struct {
		attempt  *LoginThrottleAttempt
		decision LoginThrottleDecision
	}
	results := make(chan result, 100)
	var wait sync.WaitGroup
	for range 100 {
		wait.Add(1)
		go func() {
			defer wait.Done()
			attempt, decision := throttle.BeginAttempt("192.0.2.1", "owner")
			results <- result{attempt: attempt, decision: decision}
		}()
	}
	wait.Wait()
	close(results)

	allowed := 0
	for result := range results {
		if result.decision.Allowed {
			allowed++
			throttle.CancelAttempt(result.attempt)
		}
	}
	if allowed != 5 {
		t.Fatalf("allowed attempts = %d, want exactly threshold 5", allowed)
	}
	if len(throttle.entries) != 0 || throttle.lru.Len() != 0 {
		t.Fatalf("entries/list = %d/%d after cancellation, want 0", len(throttle.entries), throttle.lru.Len())
	}
}

func TestLoginThrottleIsConcurrentAndStrictlyBounded(t *testing.T) {
	throttle, _ := newTestLoginThrottle(LoginThrottleConfig{
		FailureThreshold: 1000,
		BaseDelay:        time.Second,
		MaxDelay:         time.Minute,
		EntryTTL:         time.Hour,
		EventWindow:      time.Minute,
		MaxEntries:       32,
	})

	attempts := make(chan *LoginThrottleAttempt, 100)
	var wait sync.WaitGroup
	for i := range 100 {
		wait.Add(1)
		go func(i int) {
			defer wait.Done()
			attempt, _ := throttle.BeginAttempt(fmt.Sprintf("192.0.2.%d", i+1), fmt.Sprintf("owner-%d", i))
			attempts <- attempt
		}(i)
	}
	wait.Wait()
	close(attempts)

	if len(throttle.entries) > 32 || throttle.lru.Len() > 32 {
		t.Fatalf("entries/list = %d/%d, want at most 32", len(throttle.entries), throttle.lru.Len())
	}
	for attempt := range attempts {
		throttle.CancelAttempt(attempt)
	}
}

func TestLoginThrottleHashesNormalizedUsernameKeys(t *testing.T) {
	username := strings.Repeat("A", 1<<20)
	keys := loginThrottleKeys("", "  "+username+"  ")
	if len(keys) != 1 || len(keys[0]) != len("username:")+64 {
		t.Fatalf("keys = %#v, want one fixed-length SHA-256 key", keys)
	}
	if strings.Contains(keys[0], strings.Repeat("a", 32)) {
		t.Fatal("username plaintext leaked into throttle key")
	}
	if got, want := loginThrottleKeys("", " Owner ")[0], loginThrottleKeys("", "OWNER")[0]; got != want {
		t.Fatalf("normalized username keys differ: %q != %q", got, want)
	}
}

func recordLoginFailure(t *testing.T, throttle *LoginThrottle, ip, username string) LoginThrottleDecision {
	t.Helper()
	attempt, decision := throttle.BeginAttempt(ip, username)
	if !decision.Allowed {
		t.Fatalf("BeginAttempt(%q, %q) = %+v, want allowed", ip, username, decision)
	}
	return throttle.RecordFailure(attempt)
}

func inspectLoginThrottle(throttle *LoginThrottle, ip, username string) LoginThrottleDecision {
	attempt, decision := throttle.BeginAttempt(ip, username)
	if decision.Allowed {
		throttle.CancelAttempt(attempt)
	}
	return decision
}

func assertThrottleDecision(t *testing.T, got LoginThrottleDecision, allowed bool, retryAfter time.Duration, reportEvent bool) {
	t.Helper()
	want := LoginThrottleDecision{Allowed: allowed, RetryAfter: retryAfter, ReportEvent: reportEvent}
	if got != want {
		t.Fatalf("decision = %+v, want %+v", got, want)
	}
}
