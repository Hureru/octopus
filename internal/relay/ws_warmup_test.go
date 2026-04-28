package relay

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
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
	resetWSUpstreamPool()

	var accepted atomic.Int32
	var acceptedOnce sync.Once
	acceptedCh := make(chan struct{})
	releaseCh := make(chan struct{})
	var releaseOnce sync.Once
	closedCh := make(chan struct{})
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
		acceptedOnce.Do(func() { close(acceptedCh) })
		defer func() {
			conn.CloseNow()
			close(closedCh)
		}()
		<-releaseCh
	}))
	defer func() {
		releaseOnce.Do(func() { close(releaseCh) })
		server.Close()
		resetWSUpstreamPool()
	}()

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

	if err := bestEffortWarmupUpstreamWS(context.Background(), 321, "", reqBody); err != nil {
		t.Fatalf("bestEffortWarmupUpstreamWS failed: %v", err)
	}
	waitForWarmupAccepted(t, acceptedCh)
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

	pc := wsUpstreamPool.Get(newWSPoolKey(channel.ID, channel.Keys[0].ID, buildUpstreamWSHeaders(nil, channel, channel.Keys[0].ChannelKey)))
	if pc == nil {
		t.Fatalf("expected warmed upstream ws connection to be stored in pool")
	}
	wsUpstreamPool.Put(pc)
	releaseOnce.Do(func() { close(releaseCh) })
	waitForWarmupConnectionClosed(t, closedCh)
	wsUpstreamPool.Remove(pc.poolKey)
}

func waitForWarmupAccepted(t *testing.T, accepted <-chan struct{}) {
	t.Helper()
	select {
	case <-accepted:
	case <-time.After(2 * time.Second):
		t.Fatalf("timed out waiting for upstream ws warmup accept")
	}
}

func waitForWarmupConnectionClosed(t *testing.T, closed <-chan struct{}) {
	t.Helper()
	select {
	case <-closed:
	case <-time.After(2 * time.Second):
		t.Fatalf("timed out waiting for upstream ws warmup close")
	}
}
