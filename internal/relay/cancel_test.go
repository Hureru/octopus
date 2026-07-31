package relay

import (
	"context"
	"errors"
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

func TestIsClientCancellationIgnoresLocalRelayBudgetTimeout(t *testing.T) {
	ctx, cancel := context.WithTimeoutCause(context.Background(), 0, errLocalRelayBudgetExceeded)
	defer cancel()

	<-ctx.Done()
	if isClientCancellation(ctx, contextError(ctx)) {
		t.Fatalf("expected local relay budget timeout to not be treated as client cancellation")
	}
}

func TestIsClientCancellationIgnoresOctopusWebSocketTimeouts(t *testing.T) {
	tests := []error{
		errUpstreamWSDialTimeout,
		fmt.Errorf("dial failed: %w", errUpstreamWSDialTimeout),
		errWSClientMaxAge,
		fmt.Errorf("session ended: %w", errWSClientMaxAge),
	}
	for _, err := range tests {
		if isClientCancellation(context.Background(), err) {
			t.Fatalf("Octopus-owned timeout was treated as client cancellation: %v", err)
		}
	}

	for _, cause := range []error{errUpstreamWSDialTimeout, errWSClientMaxAge} {
		ctx, cancel := context.WithTimeoutCause(context.Background(), 0, cause)
		<-ctx.Done()
		if isClientCancellation(ctx, contextError(ctx)) {
			t.Fatalf("context cause %v was treated as client cancellation", cause)
		}
		cancel()
	}
}

func TestShouldRecordWSHealthFailure(t *testing.T) {
	t.Run("live context records upstream failures", func(t *testing.T) {
		if !shouldRecordWSHealthFailure(context.Background(), errors.New("handshake failed")) {
			t.Fatal("expected ordinary upstream failure to affect WS health")
		}
	})

	t.Run("upstream dial timeout records failure", func(t *testing.T) {
		if !shouldRecordWSHealthFailure(context.Background(), errUpstreamWSDialTimeout) {
			t.Fatal("expected upstream WS dial timeout to affect WS health")
		}
	})

	t.Run("client cancellation does not record failure", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		if shouldRecordWSHealthFailure(ctx, context.Canceled) {
			t.Fatal("expected canceled parent context to be ignored by WS health")
		}
	})

	t.Run("local relay budget does not record failure", func(t *testing.T) {
		ctx, cancel := context.WithTimeoutCause(context.Background(), 0, errLocalRelayBudgetExceeded)
		defer cancel()
		<-ctx.Done()
		if shouldRecordWSHealthFailure(ctx, contextError(ctx)) {
			t.Fatal("expected local relay budget timeout to be ignored by WS health")
		}
	})
}
