package relay

import (
	"context"
	"fmt"
	"testing"
)

func TestIsClientCancellationMatchesWrappedRequestErrors(t *testing.T) {
	ctx := context.Background()

	if !isClientCancellation(ctx, fmt.Errorf("failed to send request: %w", context.Canceled)) {
		t.Fatalf("expected wrapped context.Canceled to be treated as client cancellation")
	}
	if !isClientCancellation(ctx, fmt.Errorf("failed to send request: %w", context.DeadlineExceeded)) {
		t.Fatalf("expected wrapped context.DeadlineExceeded to be treated as client cancellation")
	}
}

func TestIsClientCancellationFallsBackToContextState(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if !isClientCancellation(ctx, fmt.Errorf("upstream request aborted")) {
		t.Fatalf("expected canceled request context to be treated as client cancellation")
	}
}

func TestIsClientCancellationIgnoresOrdinaryErrors(t *testing.T) {
	if isClientCancellation(context.Background(), fmt.Errorf("dial tcp timeout")) {
		t.Fatalf("expected ordinary upstream error to not be treated as client cancellation")
	}
}
