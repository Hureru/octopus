package relay

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	dbpkg "github.com/bestruirui/octopus/internal/db"
	"github.com/bestruirui/octopus/internal/model"
	"github.com/bestruirui/octopus/internal/op"
	"github.com/bestruirui/octopus/internal/transformer/inbound"
	"github.com/bestruirui/octopus/internal/transformer/outbound"
	"github.com/gin-gonic/gin"
)

func TestHandlerFallsBackToNextChannelAfterFirstFailure(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ctx := setupRelayTestDB(t)

	var firstHits atomic.Int32
	firstServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		firstHits.Add(1)
		http.Error(w, `{"error":"upstream unavailable"}`, http.StatusServiceUnavailable)
	}))
	defer firstServer.Close()

	var secondHits atomic.Int32
	secondServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		secondHits.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"resp_1","object":"chat.completion","created":1,"model":"fallback-model","choices":[{"index":0,"message":{"role":"assistant","content":"ok"}}]}`))
	}))
	defer secondServer.Close()

	firstChannel := &model.Channel{
		Name:     "relay-failover-first",
		Type:     outbound.OutboundTypeOpenAIChat,
		Enabled:  true,
		BaseUrls: []model.BaseUrl{{URL: firstServer.URL + "/v1"}},
		Model:    "fallback-model",
		Keys:     []model.ChannelKey{{Enabled: true, ChannelKey: "first-key"}},
	}
	if err := op.ChannelCreate(firstChannel, ctx); err != nil {
		t.Fatalf("ChannelCreate first channel failed: %v", err)
	}

	secondChannel := &model.Channel{
		Name:     "relay-failover-second",
		Type:     outbound.OutboundTypeOpenAIChat,
		Enabled:  true,
		BaseUrls: []model.BaseUrl{{URL: secondServer.URL + "/v1"}},
		Model:    "fallback-model",
		Keys:     []model.ChannelKey{{Enabled: true, ChannelKey: "second-key"}},
	}
	if err := op.ChannelCreate(secondChannel, ctx); err != nil {
		t.Fatalf("ChannelCreate second channel failed: %v", err)
	}

	group := &model.Group{
		Name:         "relay-failover-group",
		Mode:         model.GroupModeFailover,
		RetryEnabled: false,
	}
	if err := op.GroupCreate(group, ctx); err != nil {
		t.Fatalf("GroupCreate failed: %v", err)
	}
	if err := op.GroupItemAdd(&model.GroupItem{
		GroupID:   group.ID,
		ChannelID: firstChannel.ID,
		ModelName: "fallback-model",
		Priority:  1,
		Weight:    1,
	}, ctx); err != nil {
		t.Fatalf("GroupItemAdd first item failed: %v", err)
	}
	if err := op.GroupItemAdd(&model.GroupItem{
		GroupID:   group.ID,
		ChannelID: secondChannel.ID,
		ModelName: "fallback-model",
		Priority:  2,
		Weight:    1,
	}, ctx); err != nil {
		t.Fatalf("GroupItemAdd second item failed: %v", err)
	}

	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(`{"model":"relay-failover-group","messages":[{"role":"user","content":"hello"}]}`))
	c.Request.Header.Set("Content-Type", "application/json")

	Handler(inbound.InboundTypeOpenAIChat, c)

	if recorder.Code != http.StatusOK {
		t.Fatalf("expected relay handler to succeed via fallback channel, got status %d body %s", recorder.Code, recorder.Body.String())
	}
	if firstHits.Load() != 1 {
		t.Fatalf("expected first channel to be attempted once, got %d", firstHits.Load())
	}
	if secondHits.Load() != 1 {
		t.Fatalf("expected second channel to be attempted once after fallback, got %d", secondHits.Load())
	}
	if !strings.Contains(recorder.Body.String(), `"content":"ok"`) {
		t.Fatalf("expected fallback response body to be returned, got %s", recorder.Body.String())
	}
}

func setupRelayTestDB(t *testing.T) context.Context {
	t.Helper()

	if dbpkg.GetDB() != nil {
		_ = dbpkg.Close()
	}

	dbPath := filepath.Join(t.TempDir(), "octopus-relay-test.db")
	if err := dbpkg.InitDB("sqlite", dbPath, false); err != nil {
		t.Fatalf("InitDB failed: %v", err)
	}
	t.Cleanup(func() {
		_ = dbpkg.Close()
	})

	return context.Background()
}
