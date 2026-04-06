package balancer

import (
	"testing"
	"time"
)

func TestClearAllCircuitBreakersRemovesEntries(t *testing.T) {
	Reset()

	entryA := getOrCreateEntry(circuitKey(1, 11, "model-a"))
	entryA.State = StateOpen
	entryA.TripCount = 1
	entryA.LastFailureTime = time.Now()

	entryB := getOrCreateEntry(circuitKey(2, 22, "model-b"))
	entryB.State = StateOpen
	entryB.TripCount = 1
	entryB.LastFailureTime = time.Now()

	if tripped, _ := IsTripped(1, 11, "model-a"); !tripped {
		t.Fatalf("expected model-a breaker to be open before clear")
	}
	if tripped, _ := IsTripped(2, 22, "model-b"); !tripped {
		t.Fatalf("expected model-b breaker to be open before clear")
	}

	cleared := ClearAllCircuitBreakers()
	if cleared != 2 {
		t.Fatalf("expected to clear 2 breaker entries, got %d", cleared)
	}

	if tripped, _ := IsTripped(1, 11, "model-a"); tripped {
		t.Fatalf("expected model-a breaker to be cleared")
	}
	if tripped, _ := IsTripped(2, 22, "model-b"); tripped {
		t.Fatalf("expected model-b breaker to be cleared")
	}
}

func TestAbortHalfOpenReopensProbe(t *testing.T) {
	Reset()

	entry := getOrCreateEntry(circuitKey(3, 33, "model-c"))
	entry.State = StateOpen
	entry.TripCount = 1
	entry.LastFailureTime = time.Now().Add(-2 * time.Minute)

	tripped, remaining := PeekTripped(3, 33, "model-c")
	if !tripped || remaining != 0 {
		t.Fatalf("expected peek to keep open breaker blocked without countdown, got tripped=%v remaining=%v", tripped, remaining)
	}

	tripped, remaining = IsTripped(3, 33, "model-c")
	if tripped || remaining != 0 {
		t.Fatalf("expected live check to move breaker into half-open, got tripped=%v remaining=%v", tripped, remaining)
	}

	if ok := AbortHalfOpen(3, 33, "model-c"); !ok {
		t.Fatalf("expected abort to reopen half-open breaker")
	}

	tripped, remaining = IsTripped(3, 33, "model-c")
	if !tripped {
		t.Fatalf("expected breaker to be open again after abort")
	}
	if remaining <= 0 {
		t.Fatalf("expected breaker to have cooldown after abort, got %v", remaining)
	}
}
