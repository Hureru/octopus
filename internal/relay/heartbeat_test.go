package relay

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	dbmodel "github.com/bestruirui/octopus/internal/model"
	"github.com/bestruirui/octopus/internal/op"
	"github.com/gin-gonic/gin"
)

func newTestGinContext(t *testing.T) (*gin.Context, *httptest.ResponseRecorder) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", bytes.NewBufferString("{}"))
	return c, w
}

func setHeartbeatSettings(t *testing.T, interval, delay string) {
	t.Helper()
	if err := op.SettingSetString(dbmodel.SettingKeySSEHeartbeatInterval, interval); err != nil {
		t.Fatalf("set heartbeat interval failed: %v", err)
	}
	if err := op.SettingSetString(dbmodel.SettingKeySSEPreStreamHeartbeatDelay, delay); err != nil {
		t.Fatalf("set pre-stream heartbeat delay failed: %v", err)
	}
}

func TestStartEarlyHeartbeat_DisabledByZeroInterval(t *testing.T) {
	setupRelayTestDB(t)
	setHeartbeatSettings(t, "0", "1")

	c, w := newTestGinContext(t)
	hb := startEarlyHeartbeat(c, true)
	done := make(chan struct{})
	go func() {
		hb.Stop()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(500 * time.Millisecond):
		t.Fatal("Stop blocked on disabled heartbeat")
	}

	if hb.HeaderWritten() {
		t.Fatal("disabled heartbeat must not write SSE header")
	}
	if w.Body.Len() != 0 {
		t.Fatalf("disabled heartbeat wrote bytes: %q", w.Body.String())
	}
}

func TestStartEarlyHeartbeat_DisabledByZeroDelay(t *testing.T) {
	setupRelayTestDB(t)
	setHeartbeatSettings(t, "1", "0")

	c, w := newTestGinContext(t)
	hb := startEarlyHeartbeat(c, true)
	defer hb.Stop()

	if hb.HeaderWritten() {
		t.Fatal("zero delay heartbeat must not write SSE header")
	}
	if w.Body.Len() != 0 {
		t.Fatalf("zero delay heartbeat wrote bytes: %q", w.Body.String())
	}
}

func TestStartEarlyHeartbeat_DisabledByNonStream(t *testing.T) {
	setupRelayTestDB(t)
	setHeartbeatSettings(t, "1", "1")

	c, w := newTestGinContext(t)
	hb := startEarlyHeartbeat(c, false)
	defer hb.Stop()

	if hb.HeaderWritten() {
		t.Fatal("non-stream heartbeat must not write SSE header")
	}
	if w.Body.Len() != 0 {
		t.Fatalf("non-stream heartbeat wrote bytes: %q", w.Body.String())
	}
}

func TestStartEarlyHeartbeat_DoesNotWriteBeforeDelay(t *testing.T) {
	setupRelayTestDB(t)
	setHeartbeatSettings(t, "1", "1")

	c, w := newTestGinContext(t)
	hb := startEarlyHeartbeat(c, true)
	defer hb.Stop()

	time.Sleep(100 * time.Millisecond)
	if hb.HeaderWritten() {
		t.Fatal("heartbeat must not write SSE header before delay")
	}
	if w.Body.Len() != 0 {
		t.Fatalf("heartbeat wrote before delay: %q", w.Body.String())
	}
}

func TestEarlyHeartbeat_DelayedFirstHeartbeat(t *testing.T) {
	setupRelayTestDB(t)
	setHeartbeatSettings(t, "1", "1")

	c, w := newTestGinContext(t)
	hb := startEarlyHeartbeat(c, true)
	defer hb.Stop()

	time.Sleep(1200 * time.Millisecond)
	if !hb.HeaderWritten() {
		t.Fatal("expected SSE header after delay")
	}
	if got := w.Header().Get("Content-Type"); got != "text/event-stream" {
		t.Fatalf("expected Content-Type=text/event-stream, got %q", got)
	}
	body := w.Body.String()
	if count := strings.Count(body, ":\n\n"); count != 1 {
		t.Fatalf("expected exactly 1 delayed heartbeat, got %d in %q", count, body)
	}
}

func TestEarlyHeartbeat_HandBeforeDelayPreventsHeartbeat(t *testing.T) {
	setupRelayTestDB(t)
	setHeartbeatSettings(t, "1", "1")

	c, w := newTestGinContext(t)
	hb := startEarlyHeartbeat(c, true)
	hb.Hand()

	time.Sleep(1200 * time.Millisecond)
	if hb.HeaderWritten() {
		t.Fatal("Hand before delay must prevent SSE header")
	}
	if w.Body.Len() != 0 {
		t.Fatalf("Hand before delay still wrote heartbeat: %q", w.Body.String())
	}
	hb.Stop()
}

func TestEarlyHeartbeat_HandStopsTicker(t *testing.T) {
	setupRelayTestDB(t)
	setHeartbeatSettings(t, "1", "1")

	c, w := newTestGinContext(t)
	hb := startEarlyHeartbeat(c, true)
	time.Sleep(1200 * time.Millisecond)
	hb.Hand()

	initialCount := strings.Count(w.Body.String(), ":\n\n")
	if initialCount != 1 {
		t.Fatalf("expected one heartbeat before Hand, got %d", initialCount)
	}
	time.Sleep(1200 * time.Millisecond)
	finalCount := strings.Count(w.Body.String(), ":\n\n")
	if finalCount != initialCount {
		t.Fatalf("expected heartbeat count to remain %d after Hand, got %d", initialCount, finalCount)
	}

	hb.Hand()
	hb.Stop()
}

func TestEarlyHeartbeat_StopOnDisabledIsSafe(t *testing.T) {
	setupRelayTestDB(t)
	setHeartbeatSettings(t, "0", "0")
	c, _ := newTestGinContext(t)
	hb := startEarlyHeartbeat(c, true)
	hb.Stop()
	hb.Stop()
}

func TestEarlyHeartbeat_FlushOrError_SSEPath(t *testing.T) {
	setupRelayTestDB(t)
	setHeartbeatSettings(t, "1", "1")

	c, w := newTestGinContext(t)
	hb := startEarlyHeartbeat(c, true)
	defer hb.Stop()
	time.Sleep(1200 * time.Millisecond)

	hb.FlushOrError(c, http.StatusBadGateway, "channel failed")

	body := w.Body.String()
	if !strings.Contains(body, "event: error") {
		t.Fatalf("expected SSE error event, got %q", body)
	}
	if !strings.Contains(body, `"code":502`) {
		t.Fatalf("expected status code in SSE error payload, got %q", body)
	}
	if !strings.Contains(body, `"message":"channel failed"`) {
		t.Fatalf("expected error message in SSE payload, got %q", body)
	}
}

func TestEarlyHeartbeat_FlushOrError_JSONPath(t *testing.T) {
	setupRelayTestDB(t)
	setHeartbeatSettings(t, "1", "0")

	c, w := newTestGinContext(t)
	hb := startEarlyHeartbeat(c, true)
	defer hb.Stop()

	hb.FlushOrError(c, http.StatusBadGateway, "channel failed")

	body := w.Body.String()
	if strings.Contains(body, "event: error") {
		t.Fatalf("disabled heartbeat should not produce SSE error event, got %q", body)
	}
	if !strings.Contains(body, `"message":"channel failed"`) {
		t.Fatalf("expected JSON error response, got %q", body)
	}
	if got := w.Header().Get("Content-Type"); !strings.Contains(got, "application/json") {
		t.Fatalf("expected JSON Content-Type, got %q", got)
	}
}

func TestEarlyHeartbeat_NilSafe(t *testing.T) {
	var hb *earlyHeartbeat
	hb.Hand()
	hb.Stop()
	if hb.HeaderWritten() {
		t.Fatal("nil heartbeat must not report header written")
	}
	c, w := newTestGinContext(t)
	hb.FlushOrError(c, http.StatusInternalServerError, "boom")
	body := w.Body.String()
	if !strings.Contains(body, `"message":"boom"`) {
		t.Fatalf("nil heartbeat FlushOrError fallthrough failed: %q", body)
	}
}

func TestEarlyHeartbeat_TickerProducesAdditional(t *testing.T) {
	setupRelayTestDB(t)
	setHeartbeatSettings(t, "1", "1")

	c, w := newTestGinContext(t)
	hb := startEarlyHeartbeat(c, true)
	time.Sleep(2500 * time.Millisecond)
	hb.Stop()

	count := strings.Count(w.Body.String(), ":\n\n")
	if count < 2 {
		t.Fatalf("expected >=2 heartbeats over 2.5s with delay=1s interval=1s, got %d", count)
	}
}

func TestNewStreamHeartbeatTickerDoesNotFireImmediately(t *testing.T) {
	setupRelayTestDB(t)
	if err := op.SettingSetString(dbmodel.SettingKeySSEHeartbeatInterval, "1"); err != nil {
		t.Fatalf("set heartbeat interval failed: %v", err)
	}

	ticker, ch := newStreamHeartbeatTicker()
	if ticker == nil || ch == nil {
		t.Fatal("expected heartbeat ticker")
	}
	defer ticker.Stop()

	select {
	case <-ch:
		t.Fatal("stream heartbeat ticker fired immediately")
	case <-time.After(100 * time.Millisecond):
	}
}
