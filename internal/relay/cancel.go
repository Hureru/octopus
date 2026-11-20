package relay

import (
	"context"
	"errors"
)

var (
	errFirstTokenTimeout     = errors.New("first token timeout")
	errEmptyUpstreamResponse = errors.New("upstream returned an empty response")
)

func contextError(ctx context.Context) error {
	if ctx == nil {
		return nil
	}
	if cause := context.Cause(ctx); cause != nil {
		return cause
	}
	return ctx.Err()
}

func isFirstTokenTimeout(ctx context.Context, err error) bool {
	if errors.Is(err, errFirstTokenTimeout) {
		return true
	}
	if ctx == nil {
		return false
	}
	return errors.Is(context.Cause(ctx), errFirstTokenTimeout)
}

func isClientCancellation(ctx context.Context, err error) bool {
	if isFirstTokenTimeout(ctx, err) || isFirstTokenTimeout(ctx, contextError(ctx)) {
		return false
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return true
	}
	if ctx == nil {
		return false
	}
	return errors.Is(ctx.Err(), context.Canceled) || errors.Is(ctx.Err(), context.DeadlineExceeded)
}
