package relay

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/bestruirui/octopus/internal/model"
	"github.com/bestruirui/octopus/internal/op"
	"github.com/bestruirui/octopus/internal/relay/balancer"
	"github.com/bestruirui/octopus/internal/transformer/outbound"
	"github.com/coder/websocket"
	"github.com/gin-gonic/gin"
)

func TestBestEffortWarmupUpstreamWSPrimesPoolAndSticky(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ctx := setupRelayTestDB(t)

	var accepted atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/responses" {
			http.NotFound(w, r)
			return
		}
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		accepted.Add(1)
		defer conn.Close(websocket.StatusNormalClosure, "")
		<-r.Context().Done()
	}))
	defer server.Close()

	channel := &model.Channel{
		Name:     "relay-warmup-ws",
		Type:     outbound.OutboundTypeOpenAIResponse,
		Enabled:  true,
		BaseUrls: []model.BaseUrl{{URL: server.URL + "/v1"}},
		Model:    "warmup-model",
		Keys:     []model.ChannelKey{{Enabled: true, ChannelKey: "warmup-key"}},
	}
	if err := op.ChannelCreate(channel, ctx); err != nil {
		t.Fatalf("ChannelCreate failed: %v", err)
	}

	group := &model.Group{Name: "relay-warmup-group", Mode: model.GroupModeFailover, SessionKeepTime: 60}
	if err := op.GroupCreate(group, ctx); err != nil {
		t.Fatalf("GroupCreate failed: %v", err)
	}
	if err := op.GroupItemAdd(&model.GroupItem{GroupID: group.ID, ChannelID: channel.ID, ModelName: "warmup-model", Priority: 1, Weight: 1}, ctx); err != nil {
		t.Fatalf("GroupItemAdd failed: %v", err)
	}

	reqBody := map[string]json.RawMessage{
		"model":    json.RawMessage(`"relay-warmup-group"`),
		"generate": json.RawMessage(`false`),
	}
	requestHeaders := http.Header{"User-Agent": []string{"warmup-client/1.0"}}

	if err := bestEffortWarmupUpstreamWS(context.Background(), 321, "", requestHeaders, reqBody); err != nil {
		t.Fatalf("bestEffortWarmupUpstreamWS failed: %v", err)
	}
	if accepted.Load() != 1 {
		t.Fatalf("expected one upstream ws connection to be accepted, got %d", accepted.Load())
	}

	sticky := balancer.GetSticky(321, "relay-warmup-group", time.Minute)
	if sticky == nil {
		t.Fatalf("expected warmup to create sticky session")
	}
	if sticky.ChannelID != channel.ID || sticky.ChannelKeyID != channel.Keys[0].ID {
		t.Fatalf("expected sticky to target warmed channel/key, got %#v", sticky)
	}

	pc := wsUpstreamPool.Get(channel.ID, channel.Keys[0].ID, headerSignature(buildUpstreamHeaders(requestHeaders, channel, "Bearer "+channel.Keys[0].ChannelKey, true)))
	if pc == nil {
		t.Fatalf("expected warmed upstream ws connection to be stored in pool")
	}
	wsUpstreamPool.Put(channel.ID, channel.Keys[0].ID, pc)
	wsUpstreamPool.Remove(channel.ID, channel.Keys[0].ID, pc.headerSig)
}

func TestBestEffortWarmupUpstreamWSDoesNotConsumeHalfOpenProbe(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ctx := setupRelayTestDB(t)

	if err := op.SettingSetInt(model.SettingKeyCircuitBreakerThreshold, 1); err != nil {
		t.Fatalf("SettingSetInt threshold failed: %v", err)
	}
	if err := op.SettingSetInt(model.SettingKeyCircuitBreakerCooldown, 1); err != nil {
		t.Fatalf("SettingSetInt cooldown failed: %v", err)
	}
	if err := op.SettingSetInt(model.SettingKeyCircuitBreakerMaxCooldown, 8); err != nil {
		t.Fatalf("SettingSetInt max cooldown failed: %v", err)
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/responses" {
			http.NotFound(w, r)
			return
		}
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close(websocket.StatusNormalClosure, "")
		<-r.Context().Done()
	}))
	defer server.Close()

	channel := &model.Channel{
		Name:     "relay-warmup-circuit",
		Type:     outbound.OutboundTypeOpenAIResponse,
		Enabled:  true,
		BaseUrls: []model.BaseUrl{{URL: server.URL + "/v1"}},
		Model:    "warmup-circuit-model",
		Keys:     []model.ChannelKey{{Enabled: true, ChannelKey: "warmup-circuit-key"}},
	}
	if err := op.ChannelCreate(channel, ctx); err != nil {
		t.Fatalf("ChannelCreate failed: %v", err)
	}

	group := &model.Group{Name: "relay-warmup-circuit-group", Mode: model.GroupModeFailover, SessionKeepTime: 60}
	if err := op.GroupCreate(group, ctx); err != nil {
		t.Fatalf("GroupCreate failed: %v", err)
	}
	if err := op.GroupItemAdd(&model.GroupItem{GroupID: group.ID, ChannelID: channel.ID, ModelName: "warmup-circuit-model", Priority: 1, Weight: 1}, ctx); err != nil {
		t.Fatalf("GroupItemAdd failed: %v", err)
	}

	balancer.RecordFailure(channel.ID, channel.Keys[0].ID, "warmup-circuit-model", balancer.FailureHard)
	time.Sleep(1100 * time.Millisecond)

	reqBody := map[string]json.RawMessage{
		"model":    json.RawMessage(`"relay-warmup-circuit-group"`),
		"generate": json.RawMessage(`false`),
	}
	requestHeaders := http.Header{"User-Agent": []string{"warmup-client/1.0"}}

	if err := bestEffortWarmupUpstreamWS(context.Background(), 654, "", requestHeaders, reqBody); err == nil {
		t.Fatalf("expected warmup to skip open breaker instead of consuming half-open probe")
	}

	tripped, remaining := balancer.IsTripped(channel.ID, channel.Keys[0].ID, "warmup-circuit-model")
	if tripped || remaining != 0 {
		t.Fatalf("expected live request to still get the half-open probe after warmup, got tripped=%v remaining=%v", tripped, remaining)
	}
}
