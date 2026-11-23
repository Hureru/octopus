package relay

import (
	"context"
	"errors"
	"fmt"
	"net"
	"testing"
	"time"
)

func TestIsClientCancellationMatchesWrappedRequestErrors(t *testing.T) {
	ctx := context.Background()

	if !isClientCancellation(ctx, fmt.Errorf("failed to send request: %w", context.Canceled)) {
		t.Fatalf("expected wrapped context.Canceled to be treated as client cancellation")
	}
}

// 连不上上游（拨号超时）不是客户端取消：请求还活着，应该换渠道。
func TestIsClientCancellationIgnoresUpstreamTimeouts(t *testing.T) {
	_, dialErr := (&net.Dialer{Timeout: time.Nanosecond}).Dial("tcp", "127.0.0.1:1")
	if !errors.Is(dialErr, context.DeadlineExceeded) {
		t.Fatalf("expected a dial timeout that looks like a deadline, got %v", dialErr)
	}
	if isClientCancellation(context.Background(), fmt.Errorf("failed to send request: %w", dialErr)) {
		t.Fatalf("expected an upstream dial timeout to not be treated as client cancellation")
	}

	// 请求自己的时限到了，才算。
	ctx, cancel := context.WithTimeout(context.Background(), time.Nanosecond)
	defer cancel()
	<-ctx.Done()
	if !isClientCancellation(ctx, fmt.Errorf("failed to send request: %w", ctx.Err())) {
		t.Fatalf("expected an expired request context to be treated as client cancellation")
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
