package balancer

import (
	"testing"
	"time"
)

func TestResetCircuitBreakerByChannelRemovesOnlyTargetChannel(t *testing.T) {
	Reset()
	globalBreaker.Store(circuitKey(1, 10, "gpt-4o"), &circuitEntry{
		State:           StateOpen,
		LastFailureTime: time.Now(),
		TripCount:       1,
	})
	globalBreaker.Store(circuitKey(10, 10, "gpt-4o"), &circuitEntry{
		State:           StateOpen,
		LastFailureTime: time.Now(),
		TripCount:       1,
	})
	globalBreaker.Store(circuitKey(2, 20, "gpt-4o"), &circuitEntry{
		State:           StateOpen,
		LastFailureTime: time.Now(),
		TripCount:       1,
	})

	ResetStateByChannel(1)

	if tripped, _ := IsTripped(1, 10, "gpt-4o"); tripped {
		t.Fatal("expected target channel circuit breaker to be reset")
	}
	if tripped, _ := IsTripped(10, 10, "gpt-4o"); !tripped {
		t.Fatal("expected channel with similar prefix to remain tripped")
	}
	if tripped, _ := IsTripped(2, 20, "gpt-4o"); !tripped {
		t.Fatal("expected unrelated channel circuit breaker to remain tripped")
	}
}

func TestResetStickyByChannelRemovesOnlyTargetChannel(t *testing.T) {
	Reset()
	SetSticky(1, "gpt-4o", 10, 100)
	SetSticky(2, "gpt-4o", 20, 200)
	SetSticky(3, "claude", 10, 300)

	ResetStateByChannel(10)

	if entry := GetSticky(1, "gpt-4o", time.Minute); entry != nil {
		t.Fatalf("expected target channel sticky session to be reset, got %#v", entry)
	}
	if entry := GetSticky(3, "claude", time.Minute); entry != nil {
		t.Fatalf("expected second target channel sticky session to be reset, got %#v", entry)
	}
	if entry := GetSticky(2, "gpt-4o", time.Minute); entry == nil || entry.ChannelID != 20 {
		t.Fatalf("expected unrelated sticky session to remain, got %#v", entry)
	}
}

func TestHalfOpenDoesNotRemainTrippedForeverWithoutResult(t *testing.T) {
	Reset()
	key := circuitKey(7, 8, "gpt-4o")
	globalBreaker.Store(key, &circuitEntry{
		State:         StateHalfOpen,
		TripCount:     1,
		HalfOpenSince: time.Now().Add(-61 * time.Second),
	})

	tripped, remaining := IsTripped(7, 8, "gpt-4o")
	if !tripped {
		t.Fatal("expected expired half-open probe to be tripped again")
	}
	if remaining <= 0 {
		t.Fatalf("expected expired half-open probe to return cooldown, got %v", remaining)
	}

	value, ok := globalBreaker.Load(key)
	if !ok {
		t.Fatal("expected circuit entry to remain after half-open timeout")
	}
	entry := value.(*circuitEntry)
	entry.mu.Lock()
	defer entry.mu.Unlock()
	if entry.State != StateOpen {
		t.Fatalf("expected expired half-open entry to return to open, got %v", entry.State)
	}
	if !entry.HalfOpenSince.IsZero() {
		t.Fatalf("expected half-open timestamp to be cleared, got %v", entry.HalfOpenSince)
	}
}
