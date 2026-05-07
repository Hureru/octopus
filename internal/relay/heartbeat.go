package relay

import (
	"context"
	"encoding/json"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	dbmodel "github.com/bestruirui/octopus/internal/model"
	"github.com/bestruirui/octopus/internal/op"
	"github.com/bestruirui/octopus/internal/server/resp"
	"github.com/bestruirui/octopus/internal/utils/log"
	"github.com/bestruirui/octopus/internal/utils/safe"
	"github.com/gin-gonic/gin"
)

type earlyHeartbeat struct {
	c        *gin.Context
	interval time.Duration
	delay    time.Duration
	enabled  bool

	mu      sync.Mutex
	handed  atomic.Bool
	stopped atomic.Bool
	cancel  context.CancelFunc
	done    chan struct{}

	headerSet atomic.Bool
}

func startEarlyHeartbeat(c *gin.Context, isStream bool) *earlyHeartbeat {
	h := &earlyHeartbeat{c: c, done: make(chan struct{})}

	interval, intervalErr := op.SettingGetInt(dbmodel.SettingKeySSEHeartbeatInterval)
	delay, delayErr := op.SettingGetInt(dbmodel.SettingKeySSEPreStreamHeartbeatDelay)
	if intervalErr != nil || delayErr != nil || interval <= 0 || delay <= 0 || !isStream || c == nil {
		close(h.done)
		h.handed.Store(true)
		h.stopped.Store(true)
		return h
	}
	h.enabled = true
	h.interval = time.Duration(interval) * time.Second
	h.delay = time.Duration(delay) * time.Second

	ctx, cancel := context.WithCancel(c.Request.Context())
	h.cancel = cancel

	safe.Go("relay-early-heartbeat", func() {
		defer close(h.done)
		timer := time.NewTimer(h.delay)
		defer timer.Stop()
		select {
		case <-ctx.Done():
			return
		case <-timer.C:
		}

		if h.handed.Load() || h.stopped.Load() {
			return
		}
		h.mu.Lock()
		if h.handed.Load() || h.stopped.Load() {
			h.mu.Unlock()
			return
		}
		h.writeSSEHeaderLocked()
		if err := h.writeHeartbeatLocked(); err != nil {
			log.Debugf("early heartbeat initial write failed: %v", err)
			h.mu.Unlock()
			return
		}
		h.mu.Unlock()

		ticker := time.NewTicker(h.interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if h.handed.Load() || h.stopped.Load() {
					return
				}
				h.mu.Lock()
				if h.handed.Load() || h.stopped.Load() {
					h.mu.Unlock()
					return
				}
				if err := h.writeHeartbeatLocked(); err != nil {
					log.Debugf("early heartbeat write failed: %v", err)
					h.mu.Unlock()
					return
				}
				h.mu.Unlock()
			}
		}
	})
	return h
}

func (h *earlyHeartbeat) writeSSEHeaderLocked() {
	if !h.headerSet.CompareAndSwap(false, true) {
		return
	}
	w := h.c.Writer
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
	w.Flush()
}

func (h *earlyHeartbeat) writeHeartbeatLocked() error {
	if _, err := h.c.Writer.Write([]byte(":\n\n")); err != nil {
		return err
	}
	h.c.Writer.Flush()
	return nil
}

func (h *earlyHeartbeat) Hand() {
	if h == nil {
		return
	}
	if !h.handed.CompareAndSwap(false, true) {
		return
	}
	if h.cancel != nil {
		h.cancel()
	}
	<-h.done
}

func (h *earlyHeartbeat) Stop() {
	if h == nil {
		return
	}
	h.stopped.Store(true)
	if !h.handed.CompareAndSwap(false, true) {
		<-h.done
		return
	}
	if h.cancel != nil {
		h.cancel()
	}
	<-h.done
}

func (h *earlyHeartbeat) HeaderWritten() bool {
	if h == nil {
		return false
	}
	return h.headerSet.Load()
}

func (h *earlyHeartbeat) WriteSSEError(statusCode int, message string) {
	if h == nil || h.c == nil {
		return
	}
	payload, _ := json.Marshal(map[string]any{
		"code":    statusCode,
		"message": message,
	})
	h.mu.Lock()
	defer h.mu.Unlock()
	_, _ = h.c.Writer.Write([]byte("event: error\ndata: "))
	_, _ = h.c.Writer.Write(payload)
	_, _ = h.c.Writer.Write([]byte("\n\n"))
	h.c.Writer.Flush()
}

func (h *earlyHeartbeat) FlushOrError(c *gin.Context, statusCode int, message string) {
	if h != nil && h.HeaderWritten() {
		h.WriteSSEError(statusCode, message)
		return
	}
	resp.Error(c, statusCode, message)
}
