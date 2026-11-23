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
	// 不看 err 是不是 DeadlineExceeded：连不上上游、上游读超时的错误也长这样，
	// 那时请求还活着，要换渠道。请求自己的时限到了，下面的 ctx.Err() 会认出来。
	if errors.Is(err, context.Canceled) {
		return true
	}
	if ctx == nil {
		return false
	}
	return errors.Is(ctx.Err(), context.Canceled) || errors.Is(ctx.Err(), context.DeadlineExceeded)
}
