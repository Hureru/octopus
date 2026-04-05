package relay

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	dbpkg "github.com/bestruirui/octopus/internal/db"
	"github.com/bestruirui/octopus/internal/model"
	"github.com/bestruirui/octopus/internal/op"
	"github.com/bestruirui/octopus/internal/relay/balancer"
	"github.com/bestruirui/octopus/internal/transformer/inbound"
	transformerModel "github.com/bestruirui/octopus/internal/transformer/model"
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

func TestRelayMetricsUsesResponseModelForCostLookup(t *testing.T) {
	metrics := NewRelayMetrics(0, "alias-model", &transformerModel.InternalLLMRequest{Model: "alias-model"})
	metrics.StartTime = time.Now()

	metrics.SetInternalResponse(&transformerModel.InternalLLMResponse{
		Model: "gpt-4o-mini",
		Usage: &transformerModel.Usage{
			PromptTokens:     1000,
			CompletionTokens: 2000,
		},
	}, "gpt-4o-mini")

	if metrics.ActualModel != "gpt-4o-mini" {
		t.Fatalf("expected actual model to use response model, got %q", metrics.ActualModel)
	}
	if metrics.Stats.InputCost <= 0 {
		t.Fatalf("expected input cost to be computed from response model price, got %f", metrics.Stats.InputCost)
	}
	if metrics.Stats.OutputCost <= 0 {
		t.Fatalf("expected output cost to be computed from response model price, got %f", metrics.Stats.OutputCost)
	}
}

func TestHandlerRetryEnabledDoesNotTurnRecent429IntoNoAvailableKey(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ctx := setupRelayTestDB(t)

	var hits atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		w.Header().Set("Retry-After", "1")
		http.Error(w, `{"error":"rate limited"}`, http.StatusTooManyRequests)
	}))
	defer server.Close()

	channel := &model.Channel{
		Name:     "relay-retry-429",
		Type:     outbound.OutboundTypeOpenAIChat,
		Enabled:  true,
		BaseUrls: []model.BaseUrl{{URL: server.URL + "/v1"}},
		Model:    "retry-model",
		Keys: []model.ChannelKey{{
			Enabled:          true,
			ChannelKey:       "retry-key",
			StatusCode:       429,
			LastUseTimeStamp: time.Now().Unix(),
		}},
	}
	if err := op.ChannelCreate(channel, ctx); err != nil {
		t.Fatalf("ChannelCreate failed: %v", err)
	}

	group := &model.Group{
		Name:         "relay-retry-429-group",
		Mode:         model.GroupModeFailover,
		RetryEnabled: true,
		MaxRetries:   2,
	}
	if err := op.GroupCreate(group, ctx); err != nil {
		t.Fatalf("GroupCreate failed: %v", err)
	}
	if err := op.GroupItemAdd(&model.GroupItem{GroupID: group.ID, ChannelID: channel.ID, ModelName: "retry-model", Priority: 1, Weight: 1}, ctx); err != nil {
		t.Fatalf("GroupItemAdd failed: %v", err)
	}

	recorder1 := httptest.NewRecorder()
	c1, _ := gin.CreateTestContext(recorder1)
	c1.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(`{"model":"relay-retry-429-group","messages":[{"role":"user","content":"hello"}]}`))
	c1.Request.Header.Set("Content-Type", "application/json")
	Handler(inbound.InboundTypeOpenAIChat, c1)

	if recorder1.Code != http.StatusTooManyRequests {
		t.Fatalf("expected first request to pass through 429, got status %d body %s", recorder1.Code, recorder1.Body.String())
	}
	if hits.Load() != 2 {
		t.Fatalf("expected same-channel retries to attempt upstream twice, got %d", hits.Load())
	}
	if got := recorder1.Header().Get("Retry-After"); got != "1" {
		t.Fatalf("expected Retry-After header to be forwarded, got %q", got)
	}

	recorder2 := httptest.NewRecorder()
	c2, _ := gin.CreateTestContext(recorder2)
	c2.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(`{"model":"relay-retry-429-group","messages":[{"role":"user","content":"again"}]}`))
	c2.Request.Header.Set("Content-Type", "application/json")
	Handler(inbound.InboundTypeOpenAIChat, c2)

	if recorder2.Code != http.StatusTooManyRequests {
		t.Fatalf("expected second request to still reach upstream and return 429, got status %d body %s", recorder2.Code, recorder2.Body.String())
	}
	if hits.Load() != 4 {
		t.Fatalf("expected second request to retry upstream twice instead of no available key, got %d total hits", hits.Load())
	}
	if strings.Contains(recorder2.Body.String(), "no available key") {
		t.Fatalf("expected second response body not to mention no available key, got %s", recorder2.Body.String())
	}
}

func TestHandlerUsesNextKeyWhenFirstKeyCircuitIsOpen(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ctx := setupRelayTestDB(t)

	var hits atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		if got := r.Header.Get("Authorization"); got != "Bearer second-key" {
			http.Error(w, fmt.Sprintf(`{"error":"unexpected auth %q"}`, got), http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"resp_1","object":"chat.completion","created":1,"model":"multi-key-model","choices":[{"index":0,"message":{"role":"assistant","content":"ok"}}]}`))
	}))
	defer server.Close()

	channel := &model.Channel{
		Name:     "relay-multi-key-circuit",
		Type:     outbound.OutboundTypeOpenAIChat,
		Enabled:  true,
		BaseUrls: []model.BaseUrl{{URL: server.URL + "/v1"}},
		Model:    "multi-key-model",
		Keys: []model.ChannelKey{
			{Enabled: true, ChannelKey: "first-key", TotalCost: 0},
			{Enabled: true, ChannelKey: "second-key", TotalCost: 1},
		},
	}
	if err := op.ChannelCreate(channel, ctx); err != nil {
		t.Fatalf("ChannelCreate failed: %v", err)
	}

	group := &model.Group{Name: "relay-multi-key-group", Mode: model.GroupModeFailover}
	if err := op.GroupCreate(group, ctx); err != nil {
		t.Fatalf("GroupCreate failed: %v", err)
	}
	if err := op.GroupItemAdd(&model.GroupItem{GroupID: group.ID, ChannelID: channel.ID, ModelName: "multi-key-model", Priority: 1, Weight: 1}, ctx); err != nil {
		t.Fatalf("GroupItemAdd failed: %v", err)
	}

	for i := 0; i < 5; i++ {
		balancer.RecordFailure(channel.ID, channel.Keys[0].ID, "multi-key-model", balancer.FailureHard)
	}

	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(`{"model":"relay-multi-key-group","messages":[{"role":"user","content":"hello"}]}`))
	c.Request.Header.Set("Content-Type", "application/json")
	Handler(inbound.InboundTypeOpenAIChat, c)

	if recorder.Code != http.StatusOK {
		t.Fatalf("expected request to succeed via second key, got status %d body %s", recorder.Code, recorder.Body.String())
	}
	if hits.Load() != 1 {
		t.Fatalf("expected exactly one upstream call through second key, got %d", hits.Load())
	}
	if !strings.Contains(recorder.Body.String(), `"content":"ok"`) {
		t.Fatalf("expected success response body, got %s", recorder.Body.String())
	}
}

func TestSoftRateLimitFailureDoesNotTripOrAmplifyCircuitBreaker(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ctx := setupRelayTestDB(t)

	if err := op.SettingSetInt(model.SettingKeyCircuitBreakerThreshold, 2); err != nil {
		t.Fatalf("SettingSetInt threshold failed: %v", err)
	}
	if err := op.SettingSetInt(model.SettingKeyCircuitBreakerCooldown, 1); err != nil {
		t.Fatalf("SettingSetInt cooldown failed: %v", err)
	}
	if err := op.SettingSetInt(model.SettingKeyCircuitBreakerMaxCooldown, 8); err != nil {
		t.Fatalf("SettingSetInt max cooldown failed: %v", err)
	}

	var hits atomic.Int32
	var phase atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		switch phase.Load() {
		case 0:
			http.Error(w, `{"error":"server unavailable"}`, http.StatusInternalServerError)
		case 1:
			w.Header().Set("Retry-After", "1")
			http.Error(w, `{"error":"rate limited"}`, http.StatusTooManyRequests)
		default:
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"id":"resp_1","object":"chat.completion","created":1,"model":"breaker-model","choices":[{"index":0,"message":{"role":"assistant","content":"ok"}}]}`))
		}
	}))
	defer server.Close()

	channel := &model.Channel{
		Name:     "relay-soft-rate-limit",
		Type:     outbound.OutboundTypeOpenAIChat,
		Enabled:  true,
		BaseUrls: []model.BaseUrl{{URL: server.URL + "/v1"}},
		Model:    "breaker-model",
		Keys:     []model.ChannelKey{{Enabled: true, ChannelKey: "breaker-key"}},
	}
	if err := op.ChannelCreate(channel, ctx); err != nil {
		t.Fatalf("ChannelCreate failed: %v", err)
	}

	group := &model.Group{
		Name:         "relay-soft-rate-limit-group",
		Mode:         model.GroupModeFailover,
		RetryEnabled: true,
		MaxRetries:   1,
	}
	if err := op.GroupCreate(group, ctx); err != nil {
		t.Fatalf("GroupCreate failed: %v", err)
	}
	if err := op.GroupItemAdd(&model.GroupItem{GroupID: group.ID, ChannelID: channel.ID, ModelName: "breaker-model", Priority: 1, Weight: 1}, ctx); err != nil {
		t.Fatalf("GroupItemAdd failed: %v", err)
	}

	makeRequest := func(body string) *httptest.ResponseRecorder {
		recorder := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(recorder)
		c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(body))
		c.Request.Header.Set("Content-Type", "application/json")
		Handler(inbound.InboundTypeOpenAIChat, c)
		return recorder
	}

	resp1 := makeRequest(`{"model":"relay-soft-rate-limit-group","messages":[{"role":"user","content":"first"}]}`)
	if resp1.Code != http.StatusInternalServerError {
		t.Fatalf("expected first hard failure to return 500, got status %d body %s", resp1.Code, resp1.Body.String())
	}

	resp2 := makeRequest(`{"model":"relay-soft-rate-limit-group","messages":[{"role":"user","content":"second"}]}`)
	if resp2.Code != http.StatusInternalServerError {
		t.Fatalf("expected second hard failure to return 500 and trip breaker, got status %d body %s", resp2.Code, resp2.Body.String())
	}

	resp3 := makeRequest(`{"model":"relay-soft-rate-limit-group","messages":[{"role":"user","content":"third"}]}`)
	if resp3.Code != http.StatusBadGateway {
		t.Fatalf("expected open circuit to reject request before upstream call, got status %d body %s", resp3.Code, resp3.Body.String())
	}

	time.Sleep(1100 * time.Millisecond)
	phase.Store(1)
	resp4 := makeRequest(`{"model":"relay-soft-rate-limit-group","messages":[{"role":"user","content":"fourth"}]}`)
	if resp4.Code != http.StatusTooManyRequests {
		t.Fatalf("expected half-open probe to return passthrough 429, got status %d body %s", resp4.Code, resp4.Body.String())
	}
	if hits.Load() != 3 {
		t.Fatalf("expected exactly three upstream calls after soft-rate-limit probe, got %d", hits.Load())
	}

	resp5 := makeRequest(`{"model":"relay-soft-rate-limit-group","messages":[{"role":"user","content":"fifth"}]}`)
	if resp5.Code != http.StatusBadGateway {
		t.Fatalf("expected circuit to reopen after soft probe without passing, got status %d body %s", resp5.Code, resp5.Body.String())
	}

	time.Sleep(1100 * time.Millisecond)
	phase.Store(2)
	resp6 := makeRequest(`{"model":"relay-soft-rate-limit-group","messages":[{"role":"user","content":"sixth"}]}`)
	if resp6.Code != http.StatusOK {
		t.Fatalf("expected breaker to recover after second equal-length cooldown, got status %d body %s", resp6.Code, resp6.Body.String())
	}
	if hits.Load() != 4 {
		t.Fatalf("expected success probe to make one additional upstream call, got %d", hits.Load())
	}
}

func setupRelayTestDB(t *testing.T) context.Context {
	t.Helper()

	if dbpkg.GetDB() != nil {
		_ = dbpkg.Close()
	}
	balancer.Reset()

	dbPath := filepath.Join(t.TempDir(), "octopus-relay-test.db")
	if err := dbpkg.InitDB("sqlite", dbPath, false); err != nil {
		t.Fatalf("InitDB failed: %v", err)
	}
	if err := op.InitCache(); err != nil {
		t.Fatalf("InitCache failed: %v", err)
	}
	t.Cleanup(func() {
		balancer.Reset()
		_ = dbpkg.Close()
	})

	return context.Background()
}
