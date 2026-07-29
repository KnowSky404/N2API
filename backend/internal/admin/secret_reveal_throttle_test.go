package admin

import (
	"fmt"
	"sync"
	"testing"
	"time"
)

func TestSecretRevealThrottleAppliesIndependentDimensions(t *testing.T) {
	tests := []struct {
		name     string
		attempts [][3]any
	}{
		{
			name: "ip",
			attempts: [][3]any{
				{"192.0.2.1", int64(1), int64(11)},
				{"::ffff:192.0.2.1", int64(2), int64(12)},
				{"192.0.2.1", int64(3), int64(13)},
			},
		},
		{
			name: "admin",
			attempts: [][3]any{
				{"192.0.2.1", int64(1), int64(11)},
				{"192.0.2.2", int64(1), int64(12)},
				{"192.0.2.3", int64(1), int64(13)},
			},
		},
		{
			name: "key",
			attempts: [][3]any{
				{"192.0.2.1", int64(1), int64(11)},
				{"192.0.2.2", int64(2), int64(11)},
				{"192.0.2.3", int64(3), int64(11)},
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			throttle := NewSecretRevealThrottle(SecretRevealThrottleConfig{Limit: 2, Window: time.Minute, MaxEntries: 100})
			throttle.now = func() time.Time { return time.Date(2026, 7, 29, 12, 0, 0, 0, time.UTC) }
			for index, input := range test.attempts {
				decision := throttle.BeginAttempt(input[0].(string), input[1].(int64), input[2].(int64))
				if index < 2 && !decision.Allowed {
					t.Fatalf("attempt %d = %+v, want allowed", index+1, decision)
				}
				if index == 2 && (decision.Allowed || decision.RetryAfter != time.Minute) {
					t.Fatalf("attempt %d = %+v, want one-minute denial", index+1, decision)
				}
			}
		})
	}
}

func TestSecretRevealThrottleCountsEveryAdmittedAttemptAndResetsAtWindow(t *testing.T) {
	now := time.Date(2026, 7, 29, 12, 0, 0, 0, time.UTC)
	throttle := NewSecretRevealThrottle(SecretRevealThrottleConfig{Limit: 2, Window: time.Minute, MaxEntries: 100})
	throttle.now = func() time.Time { return now }

	for range 2 {
		if decision := throttle.BeginAttempt("192.0.2.1", 1, 7); !decision.Allowed {
			t.Fatalf("admitted attempt = %+v, want allowed", decision)
		}
	}
	first := throttle.BeginAttempt("192.0.2.1", 1, 7)
	second := throttle.BeginAttempt("192.0.2.1", 1, 7)
	if first.Allowed || second.Allowed || first.RetryAfter != time.Minute || second.RetryAfter != time.Minute {
		t.Fatalf("stable decisions = %+v / %+v, want one-minute denials", first, second)
	}

	now = now.Add(time.Minute)
	if decision := throttle.BeginAttempt("192.0.2.1", 1, 7); !decision.Allowed {
		t.Fatalf("post-window attempt = %+v, want allowed", decision)
	}
}

func TestSecretRevealThrottleAtomicallyBoundsConcurrentVerification(t *testing.T) {
	throttle := NewSecretRevealThrottle(SecretRevealThrottleConfig{Limit: 5, Window: time.Minute, MaxEntries: 100})
	results := make(chan SecretRevealThrottleDecision, 100)
	var wait sync.WaitGroup
	for range 100 {
		wait.Add(1)
		go func() {
			defer wait.Done()
			results <- throttle.BeginAttempt("192.0.2.1", 1, 7)
		}()
	}
	wait.Wait()
	close(results)

	allowed := 0
	for decision := range results {
		if decision.Allowed {
			allowed++
		}
	}
	if allowed != 5 {
		t.Fatalf("allowed = %d, want exactly 5", allowed)
	}
}

func TestSecretRevealThrottleFailsClosedAtBoundedCapacity(t *testing.T) {
	throttle := NewSecretRevealThrottle(SecretRevealThrottleConfig{Limit: 5, Window: time.Minute, MaxEntries: 3})
	if decision := throttle.BeginAttempt("192.0.2.1", 1, 7); !decision.Allowed {
		t.Fatalf("first attempt = %+v, want allowed", decision)
	}
	if got := len(throttle.entries); got != 3 {
		t.Fatalf("entries = %d, want 3", got)
	}
	decision := throttle.BeginAttempt("192.0.2.2", 2, 8)
	if decision.Allowed || decision.RetryAfter != time.Minute {
		t.Fatalf("capacity decision = %+v, want one-minute denial", decision)
	}
	if got := len(throttle.entries); got != 3 {
		t.Fatalf("entries = %d after denial, want 3", got)
	}
}

func TestSecretRevealThrottleNeverExceedsConfiguredStorageUnderConcurrency(t *testing.T) {
	throttle := NewSecretRevealThrottle(SecretRevealThrottleConfig{Limit: 5, Window: time.Minute, MaxEntries: 32})
	var wait sync.WaitGroup
	for index := range 100 {
		wait.Add(1)
		go func(index int) {
			defer wait.Done()
			throttle.BeginAttempt(fmt.Sprintf("192.0.2.%d", index+1), int64(index+1), int64(index+1))
		}(index)
	}
	wait.Wait()
	if len(throttle.entries) > 32 || throttle.order.Len() > 32 {
		t.Fatalf("entries/order = %d/%d, want at most 32", len(throttle.entries), throttle.order.Len())
	}
}
