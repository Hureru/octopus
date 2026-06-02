package relay

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"sync"
	"time"

	"github.com/bestruirui/octopus/internal/utils/log"
)

type firstTokenBudget struct {
	ctx     context.Context
	timer   *time.Timer
	cancel  context.CancelCauseFunc
	mu      sync.Mutex
	stopped bool
	once    sync.Once
}

func (b *firstTokenBudget) stopTimer() {
	if b == nil {
		return
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.stopped {
		return
	}
	b.stopped = true
	if b.timer == nil {
		return
	}
	b.timer.Stop()
}

func (b *firstTokenBudget) close() {
	if b == nil {
		return
	}
	b.once.Do(func() {
		b.stopTimer()
		if b.cancel != nil {
			b.cancel(context.Canceled)
		}
	})
}

func (ra *relayAttempt) attachFirstTokenBudget(req *http.Request) *http.Request {
	if req == nil || !ra.shouldUseFirstTokenBudget() {
		return req
	}

	ctx, cancel := context.WithCancelCause(req.Context())
	budget := &firstTokenBudget{ctx: ctx, cancel: cancel}
	budget.timer = time.AfterFunc(time.Duration(ra.firstTokenTimeOutSec)*time.Second, func() {
		budget.mu.Lock()
		defer budget.mu.Unlock()
		if budget.stopped {
			return
		}
		cancel(errFirstTokenTimeout)
	})
	ra.firstTokenBudget = budget
	return req.WithContext(ctx)
}

func (ra *relayAttempt) shouldUseFirstTokenBudget() bool {
	return ra != nil &&
		ra.firstTokenTimeOutSec > 0 &&
		ra.internalRequest != nil &&
		ra.internalRequest.Stream != nil &&
		*ra.internalRequest.Stream
}

func (ra *relayAttempt) stopFirstTokenTimer() {
	if ra == nil || ra.firstTokenBudget == nil {
		return
	}
	ra.firstTokenBudget.stopTimer()
}

func (ra *relayAttempt) closeFirstTokenBudget() {
	if ra == nil || ra.firstTokenBudget == nil {
		return
	}
	ra.firstTokenBudget.close()
}

func (ra *relayAttempt) firstTokenTimeoutError() error {
	if ra == nil || ra.firstTokenTimeOutSec <= 0 {
		return errFirstTokenTimeout
	}
	return fmt.Errorf("%w (%ds)", errFirstTokenTimeout, ra.firstTokenTimeOutSec)
}

func (ra *relayAttempt) firstTokenTimeoutIfNeeded(ctx context.Context, err error) error {
	budgetCtx := context.Context(nil)
	if ra != nil && ra.firstTokenBudget != nil {
		budgetCtx = ra.firstTokenBudget.ctx
	}
	if isFirstTokenTimeout(ctx, err) || isFirstTokenTimeout(ctx, contextError(ctx)) ||
		isFirstTokenTimeout(budgetCtx, err) || isFirstTokenTimeout(budgetCtx, contextError(budgetCtx)) {
		if ra != nil && ra.firstTokenTimeOutSec > 0 {
			log.Warnf("first token timeout (%ds), switching channel", ra.firstTokenTimeOutSec)
		}
		return ra.firstTokenTimeoutError()
	}
	return nil
}

type closeWithFuncReadCloser struct {
	io.ReadCloser
	onClose func()
}

func (c *closeWithFuncReadCloser) Close() error {
	err := c.ReadCloser.Close()
	if c.onClose != nil {
		c.onClose()
	}
	return err
}
