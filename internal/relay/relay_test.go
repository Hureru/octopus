package relay

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
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
	"github.com/bestruirui/octopus/internal/utils/tokenizer"
	"github.com/coder/websocket"
	"github.com/gin-gonic/gin"
)

func TestHandleStreamResponsePassthroughAnthropicPreservesRawSSE(t *testing.T) {
	gin.SetMode(gin.TestMode)

	rawSSE := strings.Join([]string{
		"event: message_start",
		`data: {"type":"message_start","message":{"id":"msg_1","type":"message","role":"assistant","model":"claude-haiku-4-5-20251001","content":[]}}`,
		"",
		"event: content_block_start",
		`data: {"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}`,
		"",
		"event: content_block_delta",
		`data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"hello"}}`,
		"",
		"event: content_block_stop",
		`data: {"type":"content_block_stop","index":0}`,
		"",
		"event: message_delta",
		`data: {"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":1}}`,
		"",
		"event: message_stop",
		`data: {"type":"message_stop"}`,
		"",
	}, "\n")

	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", nil)
	internalReq := &transformerModel.InternalLLMRequest{
		Model:        "claude-haiku-4-5-20251001",
		Stream:       boolPtr(true),
		RawAPIFormat: transformerModel.APIFormatAnthropicMessage,
	}
	req := &relayRequest{
		c:               c,
		inAdapter:       inbound.Get(inbound.InboundTypeAnthropic),
		internalRequest: internalReq,
		metrics:         NewRelayMetrics(1, internalReq.Model, nil, internalReq),
		apiKeyID:        1,
		requestModel:    internalReq.Model,
	}
	ra := &relayAttempt{
		relayRequest: req,
		outAdapter:   outbound.Get(outbound.OutboundTypeAnthropic),
	}

	response := &http.Response{
		StatusCode: http.StatusOK,
		Header: http.Header{
			"Content-Type": []string{"text/event-stream"},
		},
		Body: io.NopCloser(bytes.NewReader([]byte(rawSSE))),
	}

	if err := ra.handleStreamResponsePassthroughAnthropic(context.Background(), response); err != nil {
		t.Fatalf("handleStreamResponsePassthroughAnthropic() error = %v", err)
	}

	if got := recorder.Body.String(); got != rawSSE {
		t.Fatalf("expected raw SSE to be preserved exactly, got %q want %q", got, rawSSE)
	}
}

func TestHandleStreamResponsePassthroughOpenAIResponsesPreservesRawSSE(t *testing.T) {
	gin.SetMode(gin.TestMode)

	rawSSE := strings.Join([]string{
		`data: {"type":"response.created","response":{"id":"resp_1","object":"response","model":"gpt-4o","created_at":1,"output":[],"status":"in_progress"}}`,
		"",
		`data: {"type":"response.output_text.delta","delta":"hello"}`,
		"",
		`data: {"type":"response.completed","response":{"id":"resp_1","object":"response","model":"gpt-4o","created_at":1,"output":[],"status":"completed"}}`,
		"",
	}, "\n")

	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	internalReq := &transformerModel.InternalLLMRequest{
		Model:        "gpt-4o",
		Stream:       boolPtr(true),
		RawAPIFormat: transformerModel.APIFormatOpenAIResponse,
	}
	req := &relayRequest{
		c:               c,
		inAdapter:       inbound.Get(inbound.InboundTypeOpenAIResponse),
		internalRequest: internalReq,
		metrics:         NewRelayMetrics(1, internalReq.Model, nil, internalReq),
		apiKeyID:        1,
		requestModel:    internalReq.Model,
	}
	ra := &relayAttempt{
		relayRequest: req,
		outAdapter:   outbound.Get(outbound.OutboundTypeOpenAIResponse),
		channel:      &model.Channel{Type: outbound.OutboundTypeOpenAIResponse},
	}

	response := &http.Response{
		StatusCode: http.StatusOK,
		Header: http.Header{
			"Content-Type": []string{"text/event-stream"},
		},
		Body: io.NopCloser(bytes.NewReader([]byte(rawSSE))),
	}

	if err := ra.handleStreamResponsePassthroughOpenAIResponses(context.Background(), response); err != nil {
		t.Fatalf("handleStreamResponsePassthroughOpenAIResponses() error = %v", err)
	}
	if got := recorder.Body.String(); got != rawSSE {
		t.Fatalf("expected raw SSE to be preserved exactly, got %q want %q", got, rawSSE)
	}
}

func TestHandlerPassthroughsOpenAIResponsesSameProtocolStream(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ctx := setupRelayTestDB(t)

	rawSSE := strings.Join([]string{
		`data: {"type":"response.created","response":{"id":"resp_1","object":"response","model":"gpt-4o","created_at":1,"output":[],"status":"in_progress"}}`,
		"",
		`event: response.custom_debug`,
		`data: {"type":"response.custom_debug","custom":{"keep":true}}`,
		"",
		`data: {"type":"response.output_text.delta","delta":"hello"}`,
		"",
		`data: {"type":"response.completed","response":{"id":"resp_1","object":"response","model":"gpt-4o","created_at":1,"output":[{"type":"message","role":"assistant","content":[{"type":"output_text","text":"hello"}]}],"status":"completed","usage":{"input_tokens":1,"output_tokens":1,"total_tokens":2}}}`,
		"",
	}, "\n")

	var capturedBody []byte
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatalf("read upstream request body failed: %v", err)
		}
		capturedBody = append([]byte(nil), body...)
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte(rawSSE))
	}))
	defer server.Close()

	channel := &model.Channel{
		Name:     "relay-openai-responses-same-protocol-stream",
		Type:     outbound.OutboundTypeOpenAIResponse,
		Enabled:  true,
		BaseUrls: []model.BaseUrl{{URL: server.URL + "/v1"}},
		Model:    "gpt-4o",
		Keys:     []model.ChannelKey{{Enabled: true, ChannelKey: "test-key"}},
	}
	if err := op.ChannelCreate(channel, ctx); err != nil {
		t.Fatalf("ChannelCreate failed: %v", err)
	}

	group := &model.Group{Name: "relay-openai-responses-same-protocol-stream-group", Mode: model.GroupModeFailover}
	if err := op.GroupCreate(group, ctx); err != nil {
		t.Fatalf("GroupCreate failed: %v", err)
	}
	if err := op.GroupItemAdd(&model.GroupItem{GroupID: group.ID, ChannelID: channel.ID, ModelName: "gpt-4o", Priority: 1, Weight: 1}, ctx); err != nil {
		t.Fatalf("GroupItemAdd failed: %v", err)
	}

	requestBody := `{"model":"relay-openai-responses-same-protocol-stream-group","input":"hello","stream":true}`
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Set("api_key_id", 7)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(requestBody))
	c.Request.Header.Set("Content-Type", "application/json")

	Handler(inbound.InboundTypeOpenAIResponse, c)

	if recorder.Code != http.StatusOK {
		t.Fatalf("expected request to succeed, got status %d body %s", recorder.Code, recorder.Body.String())
	}
	if got := recorder.Body.String(); got != rawSSE {
		t.Fatalf("expected raw SSE to be preserved exactly, got %q want %q", got, rawSSE)
	}
	var payload map[string]any
	if err := json.Unmarshal(capturedBody, &payload); err != nil {
		t.Fatalf("unmarshal upstream request failed: %v", err)
	}
	if payload["model"] != "gpt-4o" {
		t.Fatalf("expected model to be rewritten to upstream model, got %#v", payload["model"])
	}
	if _, ok := payload["tools"]; ok {
		t.Fatalf("ordinary same-protocol request should not require native tools to passthrough, got %#v", payload["tools"])
	}
}

func TestHandlerPassthroughsOpenAIResponsesSameProtocolNonStream(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ctx := setupRelayTestDB(t)

	rawResponse := `{"id":"resp_1","object":"response","created_at":1,"model":"gpt-4o","output":[{"type":"message","role":"assistant","content":[{"type":"output_text","text":"ok","annotations":[{"type":"custom_annotation","keep":true}]}]}],"status":"completed","custom_top_level":{"keep":true}}`
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(rawResponse))
	}))
	defer server.Close()

	channel := &model.Channel{
		Name:     "relay-openai-responses-same-protocol-non-stream",
		Type:     outbound.OutboundTypeOpenAIResponse,
		Enabled:  true,
		BaseUrls: []model.BaseUrl{{URL: server.URL + "/v1"}},
		Model:    "gpt-4o",
		Keys:     []model.ChannelKey{{Enabled: true, ChannelKey: "test-key"}},
	}
	if err := op.ChannelCreate(channel, ctx); err != nil {
		t.Fatalf("ChannelCreate failed: %v", err)
	}

	group := &model.Group{Name: "relay-openai-responses-same-protocol-non-stream-group", Mode: model.GroupModeFailover}
	if err := op.GroupCreate(group, ctx); err != nil {
		t.Fatalf("GroupCreate failed: %v", err)
	}
	if err := op.GroupItemAdd(&model.GroupItem{GroupID: group.ID, ChannelID: channel.ID, ModelName: "gpt-4o", Priority: 1, Weight: 1}, ctx); err != nil {
		t.Fatalf("GroupItemAdd failed: %v", err)
	}

	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Set("api_key_id", 7)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(`{"model":"relay-openai-responses-same-protocol-non-stream-group","input":"hello"}`))
	c.Request.Header.Set("Content-Type", "application/json")

	Handler(inbound.InboundTypeOpenAIResponse, c)

	if recorder.Code != http.StatusOK {
		t.Fatalf("expected request to succeed, got status %d body %s", recorder.Code, recorder.Body.String())
	}
	if got := recorder.Body.String(); got != rawResponse {
		t.Fatalf("expected raw JSON to be preserved exactly, got %q want %q", got, rawResponse)
	}
}

func TestHandlerPassthroughsOpenAIResponsesRawTools(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ctx := setupRelayTestDB(t)

	var capturedBody []byte
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatalf("read upstream request body failed: %v", err)
		}
		capturedBody = append([]byte(nil), body...)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"resp_1","object":"response","created_at":1,"model":"gpt-4o","output":[],"status":"completed"}`))
	}))
	defer server.Close()

	channel := &model.Channel{
		Name:     "relay-openai-responses-passthrough",
		Type:     outbound.OutboundTypeOpenAIResponse,
		Enabled:  true,
		BaseUrls: []model.BaseUrl{{URL: server.URL + "/v1"}},
		Model:    "gpt-4o",
		Keys:     []model.ChannelKey{{Enabled: true, ChannelKey: "test-key"}},
	}
	if err := op.ChannelCreate(channel, ctx); err != nil {
		t.Fatalf("ChannelCreate failed: %v", err)
	}

	group := &model.Group{Name: "relay-openai-responses-passthrough-group", Mode: model.GroupModeFailover}
	if err := op.GroupCreate(group, ctx); err != nil {
		t.Fatalf("GroupCreate failed: %v", err)
	}
	if err := op.GroupItemAdd(&model.GroupItem{GroupID: group.ID, ChannelID: channel.ID, ModelName: "gpt-4o", Priority: 1, Weight: 1}, ctx); err != nil {
		t.Fatalf("GroupItemAdd failed: %v", err)
	}

	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Set("api_key_id", 7)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(`{"model":"relay-openai-responses-passthrough-group","input":"hello","tools":[{"type":"apply_patch"}]}`))
	c.Request.Header.Set("Content-Type", "application/json")

	Handler(inbound.InboundTypeOpenAIResponse, c)

	if recorder.Code != http.StatusOK {
		t.Fatalf("expected request to succeed, got status %d body %s", recorder.Code, recorder.Body.String())
	}
	if len(capturedBody) == 0 {
		t.Fatalf("expected upstream request body to be captured")
	}

	var payload map[string]any
	if err := json.Unmarshal(capturedBody, &payload); err != nil {
		t.Fatalf("unmarshal upstream request failed: %v", err)
	}
	if payload["model"] != "gpt-4o" {
		t.Fatalf("expected model to be rewritten to upstream model, got %#v", payload["model"])
	}
	tools, ok := payload["tools"].([]any)
	if !ok || len(tools) != 1 {
		t.Fatalf("expected raw tool definition to survive passthrough, got %#v", payload["tools"])
	}
	tool, ok := tools[0].(map[string]any)
	if !ok || tool["type"] != "apply_patch" {
		t.Fatalf("expected apply_patch tool to be preserved, got %#v", tools[0])
	}
}

func TestHandlerRejectsResponsesNativeToolsWithoutResponsesChannel(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ctx := setupRelayTestDB(t)

	channel := &model.Channel{
		Name:     "relay-openai-chat-only",
		Type:     outbound.OutboundTypeOpenAIChat,
		Enabled:  true,
		BaseUrls: []model.BaseUrl{{URL: "https://example.com/v1"}},
		Model:    "gpt-4o",
		Keys:     []model.ChannelKey{{Enabled: true, ChannelKey: "test-key"}},
	}
	if err := op.ChannelCreate(channel, ctx); err != nil {
		t.Fatalf("ChannelCreate failed: %v", err)
	}

	group := &model.Group{Name: "relay-openai-chat-only-group", Mode: model.GroupModeFailover}
	if err := op.GroupCreate(group, ctx); err != nil {
		t.Fatalf("GroupCreate failed: %v", err)
	}
	if err := op.GroupItemAdd(&model.GroupItem{GroupID: group.ID, ChannelID: channel.ID, ModelName: "gpt-4o", Priority: 1, Weight: 1}, ctx); err != nil {
		t.Fatalf("GroupItemAdd failed: %v", err)
	}

	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Set("api_key_id", 8)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(`{"model":"relay-openai-chat-only-group","input":"hello","tools":[{"type":"apply_patch"}]}`))
	c.Request.Header.Set("Content-Type", "application/json")

	Handler(inbound.InboundTypeOpenAIResponse, c)

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("expected native responses tool request to be rejected, got status %d body %s", recorder.Code, recorder.Body.String())
	}
	if !strings.Contains(recorder.Body.String(), "仅支持 OpenAI Responses 通道直通") {
		t.Fatalf("expected clear passthrough-only error, got %s", recorder.Body.String())
	}
}

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
	metrics := NewRelayMetrics(0, "alias-model", nil, &transformerModel.InternalLLMRequest{Model: "alias-model"})
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

func TestRelayMetricsCapturesOpenAICompatibleInputBreakdown(t *testing.T) {
	metrics := NewRelayMetrics(0, "alias-model", nil, &transformerModel.InternalLLMRequest{Model: "alias-model"})
	payload := []byte(`{"model":"gpt-4o-mini","input":"hello world"}`)
	metrics.SetTransportRequestPayload(payload, "gpt-4o-mini")
	metrics.SetInternalResponse(&transformerModel.InternalLLMResponse{
		Model: "gpt-4o-mini",
		Usage: &transformerModel.Usage{
			PromptTokens:     1200,
			CompletionTokens: 300,
			PromptTokensDetails: &transformerModel.PromptTokensDetails{
				CachedTokens: 900,
			},
		},
	}, "gpt-4o-mini")

	if metrics.TransportInputTokens == nil || *metrics.TransportInputTokens != tokenizer.CountTokens(string(payload), "gpt-4o-mini") {
		t.Fatalf("expected transport input tokens to be estimated from payload, got %#v", metrics.TransportInputTokens)
	}
	if metrics.BillInputTokens == nil || *metrics.BillInputTokens != 300 {
		t.Fatalf("expected billed input tokens to exclude cache read tokens, got %#v", metrics.BillInputTokens)
	}
	if metrics.CacheReadTokens == nil || *metrics.CacheReadTokens != 900 {
		t.Fatalf("expected cache read tokens to be captured, got %#v", metrics.CacheReadTokens)
	}
	if metrics.CacheWriteTokens == nil || *metrics.CacheWriteTokens != 0 {
		t.Fatalf("expected cache write tokens to default to zero, got %#v", metrics.CacheWriteTokens)
	}
}

func TestRelayMetricsCapturesAnthropicInputBreakdown(t *testing.T) {
	metrics := NewRelayMetrics(0, "alias-model", nil, &transformerModel.InternalLLMRequest{Model: "alias-model"})
	metrics.SetInternalResponse(&transformerModel.InternalLLMResponse{
		Model: "claude-sonnet-4-5",
		Usage: &transformerModel.Usage{
			PromptTokens:             400,
			CompletionTokens:         180,
			CacheCreationInputTokens: 250,
			CacheReadInputTokens:     1200,
			PromptTokensDetails: &transformerModel.PromptTokensDetails{
				CachedTokens: 1200,
			},
		},
	}, "claude-sonnet-4-5")

	if metrics.BillInputTokens == nil || *metrics.BillInputTokens != 400 {
		t.Fatalf("expected anthropic billed input tokens to keep prompt tokens as-is, got %#v", metrics.BillInputTokens)
	}
	if metrics.CacheReadTokens == nil || *metrics.CacheReadTokens != 1200 {
		t.Fatalf("expected anthropic cache read tokens to be captured, got %#v", metrics.CacheReadTokens)
	}
	if metrics.CacheWriteTokens == nil || *metrics.CacheWriteTokens != 250 {
		t.Fatalf("expected anthropic cache write tokens to be captured, got %#v", metrics.CacheWriteTokens)
	}
}

func TestDefaultWSModeForRequest(t *testing.T) {
	previousResponseID := "resp_123"
	if got := defaultWSModeForRequest(&transformerModel.InternalLLMRequest{PreviousResponseID: &previousResponseID}); got != model.RelayLogWSModeContinuation {
		t.Fatalf("expected previous_response_id request to be marked as continuation, got %q", got)
	}
	if got := defaultWSModeForRequest(&transformerModel.InternalLLMRequest{Messages: []transformerModel.Message{{Role: "user"}}}); got != model.RelayLogWSModeFresh {
		t.Fatalf("expected ordinary request to be marked as fresh, got %q", got)
	}
}

func TestHandlerStopsFailoverWhenContinuationTransportIsUnavailable(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ctx := setupRelayTestDB(t)
	if err := op.SettingSetString(model.SettingKeyRelayWSUpgradeEnabled, "true"); err != nil {
		t.Fatalf("SettingSetString relay ws upgrade failed: %v", err)
	}

	var secondHits atomic.Int32
	firstChannel := &model.Channel{
		Name:     "relay-ws-continuation-first",
		Type:     outbound.OutboundTypeOpenAIResponse,
		Enabled:  true,
		BaseUrls: []model.BaseUrl{{URL: "https://first.example/v1"}},
		Model:    "gpt-4o",
		Keys:     []model.ChannelKey{{Enabled: true, ChannelKey: "first-key"}},
	}
	if err := op.ChannelCreate(firstChannel, ctx); err != nil {
		t.Fatalf("ChannelCreate first channel failed: %v", err)
	}

	secondServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		secondHits.Add(1)
		http.Error(w, `{"error":"should not be reached"}`, http.StatusServiceUnavailable)
	}))
	defer secondServer.Close()

	secondChannel := &model.Channel{
		Name:     "relay-ws-continuation-second",
		Type:     outbound.OutboundTypeOpenAIResponse,
		Enabled:  true,
		BaseUrls: []model.BaseUrl{{URL: secondServer.URL + "/v1"}},
		Model:    "gpt-4o",
		Keys:     []model.ChannelKey{{Enabled: true, ChannelKey: "second-key"}},
	}
	if err := op.ChannelCreate(secondChannel, ctx); err != nil {
		t.Fatalf("ChannelCreate second channel failed: %v", err)
	}

	group := &model.Group{Name: "relay-ws-continuation-group", Mode: model.GroupModeFailover, SessionKeepTime: 60}
	if err := op.GroupCreate(group, ctx); err != nil {
		t.Fatalf("GroupCreate failed: %v", err)
	}
	if err := op.GroupItemAdd(&model.GroupItem{GroupID: group.ID, ChannelID: firstChannel.ID, ModelName: "gpt-4o", Priority: 1, Weight: 1}, ctx); err != nil {
		t.Fatalf("GroupItemAdd first item failed: %v", err)
	}
	if err := op.GroupItemAdd(&model.GroupItem{GroupID: group.ID, ChannelID: secondChannel.ID, ModelName: "gpt-4o", Priority: 2, Weight: 1}, ctx); err != nil {
		t.Fatalf("GroupItemAdd second item failed: %v", err)
	}

	balancer.SetSticky(77, "relay-ws-continuation-group", firstChannel.ID, firstChannel.Keys[0].ID)

	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Set("api_key_id", 77)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(`{"model":"relay-ws-continuation-group","previous_response_id":"resp_prev","input":"hello","stream":true}`))
	c.Request.Header.Set("Content-Type", "application/json")

	// 创建并立即关闭一个连接，模拟池里残留的失效上游 WS。
	wsServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		conn.Close(websocket.StatusNormalClosure, "")
	}))
	defer wsServer.Close()

	firstChannel.BaseUrls = []model.BaseUrl{{URL: wsServer.URL + "/v1"}}
	if _, err := op.ChannelUpdate(&model.ChannelUpdateRequest{ID: firstChannel.ID, BaseUrls: &firstChannel.BaseUrls}, ctx); err != nil {
		t.Fatalf("ChannelUpdate first channel failed: %v", err)
	}

	pc := TryUpstreamWS(context.Background(), firstChannel, firstChannel.GetBaseUrl(), firstChannel.Keys[0].ChannelKey, firstChannel.Keys[0].ID, c.Request.Header, true)
	if pc == nil {
		t.Fatalf("expected initial ws dial to succeed")
	}
	pc.conn.Close(websocket.StatusNormalClosure, "")
	wsUpstreamPool.Put(pc)

	Handler(inbound.InboundTypeOpenAIResponse, c)

	if recorder.Code != http.StatusConflict {
		t.Fatalf("expected continuation transport failure to return 409, got %d body %s", recorder.Code, recorder.Body.String())
	}
	if !strings.Contains(recorder.Body.String(), "上游连续会话已中断") {
		t.Fatalf("expected conversation reset error response body, got %s", recorder.Body.String())
	}
	if secondHits.Load() != 0 {
		t.Fatalf("expected failover to stop before hitting second channel, got %d hits", secondHits.Load())
	}
	if sticky := balancer.GetSticky(77, "relay-ws-continuation-group", time.Minute); sticky != nil {
		t.Fatalf("expected sticky to be cleared after continuation failure, got %#v", sticky)
	}
	wsUpstreamPool.Remove(pc.poolKey)
	wsUpstreamPool.Remove(newWSPoolKey(secondChannel.ID, secondChannel.Keys[0].ID, buildUpstreamWSHeaders(c.Request.Header, secondChannel, secondChannel.Keys[0].ChannelKey)))
}

func TestForwardViaWSRedialsFreshRequestAfterStalePooledConnection(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ctx := setupRelayTestDB(t)

	var accepted atomic.Int32
	wsServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		accepted.Add(1)
		defer conn.Close(websocket.StatusNormalClosure, "")

		_, _, err = conn.Read(r.Context())
		if err != nil {
			return
		}

		_ = conn.Write(r.Context(), websocket.MessageText, []byte(`{"type":"response.created","response":{"id":"resp_new","model":"gpt-4o"}}`))
		_ = conn.Write(r.Context(), websocket.MessageText, []byte(`{"type":"response.output_text.delta","delta":"ok"}`))
		_ = conn.Write(r.Context(), websocket.MessageText, []byte(`{"type":"response.completed","response":{"id":"resp_new","model":"gpt-4o","status":"completed","usage":{"input_tokens":1,"output_tokens":1,"total_tokens":2}}}`))
	}))
	defer wsServer.Close()

	channel := &model.Channel{
		Name:     "relay-ws-redial",
		Type:     outbound.OutboundTypeOpenAIResponse,
		Enabled:  true,
		BaseUrls: []model.BaseUrl{{URL: wsServer.URL + "/v1"}},
		Model:    "gpt-4o",
		Keys:     []model.ChannelKey{{Enabled: true, ChannelKey: "fresh-key"}},
	}
	if err := op.ChannelCreate(channel, ctx); err != nil {
		t.Fatalf("ChannelCreate failed: %v", err)
	}

	stale := TryUpstreamWS(context.Background(), channel, channel.GetBaseUrl(), channel.Keys[0].ChannelKey, channel.Keys[0].ID, nil, true)
	if stale == nil {
		t.Fatalf("expected initial ws dial to succeed")
	}
	stale.conn.Close(websocket.StatusNormalClosure, "")
	wsUpstreamPool.Put(stale)

	writer := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(writer)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	internalReq := &transformerModel.InternalLLMRequest{Model: "gpt-4o", Stream: boolPtr(true)}
	req := &relayRequest{
		c:               c,
		inAdapter:       inbound.Get(inbound.InboundTypeOpenAIResponse),
		internalRequest: internalReq,
		metrics:         NewRelayMetrics(1, "gpt-4o", nil, internalReq),
		apiKeyID:        1,
		requestModel:    "gpt-4o",
	}
	ra := &relayAttempt{
		relayRequest: req,
		outAdapter:   outbound.Get(channel.Type),
		channel:      channel,
		usedKey:      channel.Keys[0],
	}

	statusCode, err := ra.forwardViaWS(context.Background())
	if err != nil {
		t.Fatalf("expected fresh ws request to recover by redial, got err %v", err)
	}
	if statusCode != http.StatusOK {
		t.Fatalf("expected fresh ws request to succeed after redial, got %d", statusCode)
	}
	if accepted.Load() < 2 {
		t.Fatalf("expected stale connection plus forced redial, got %d accepted connections", accepted.Load())
	}
	if req.metrics.WSRecovery == nil || *req.metrics.WSRecovery != model.RelayLogWSRecoveryReconnect {
		t.Fatalf("expected ws reconnect recovery to be recorded, got %#v", req.metrics.WSRecovery)
	}
	wsUpstreamPool.Remove(stale.poolKey)
}

func TestForwardViaWSReconnectsContinuationAfterReadFailureBeforeFirstEvent(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ctx := setupRelayTestDB(t)

	var accepted atomic.Int32
	wsServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		accepted.Add(1)
		defer conn.Close(websocket.StatusNormalClosure, "")

		_, _, err = conn.Read(r.Context())
		if err != nil {
			return
		}

		if accepted.Load() == 1 {
			conn.Close(websocket.StatusNormalClosure, "")
			return
		}

		_ = conn.Write(r.Context(), websocket.MessageText, []byte(`{"type":"response.created","response":{"id":"resp_cont_new","model":"gpt-4o"}}`))
		_ = conn.Write(r.Context(), websocket.MessageText, []byte(`{"type":"response.output_text.delta","delta":"ok"}`))
		_ = conn.Write(r.Context(), websocket.MessageText, []byte(`{"type":"response.completed","response":{"id":"resp_cont_new","model":"gpt-4o","status":"completed","usage":{"input_tokens":1,"output_tokens":1,"total_tokens":2}}}`))
	}))
	defer wsServer.Close()

	channel := &model.Channel{
		Name:     "relay-ws-cont-read-reconnect",
		Type:     outbound.OutboundTypeOpenAIResponse,
		Enabled:  true,
		BaseUrls: []model.BaseUrl{{URL: wsServer.URL + "/v1"}},
		Model:    "gpt-4o",
		Keys:     []model.ChannelKey{{Enabled: true, ChannelKey: "cont-key"}},
	}
	if err := op.ChannelCreate(channel, ctx); err != nil {
		t.Fatalf("ChannelCreate failed: %v", err)
	}

	writer := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(writer)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	internalReq := &transformerModel.InternalLLMRequest{Model: "gpt-4o", Stream: boolPtr(true), PreviousResponseID: stringPtr("resp_prev")}
	req := &relayRequest{
		c:               c,
		inAdapter:       inbound.Get(inbound.InboundTypeOpenAIResponse),
		internalRequest: internalReq,
		metrics:         NewRelayMetrics(1, "gpt-4o", nil, internalReq),
		apiKeyID:        1,
		requestModel:    "gpt-4o",
	}
	ra := &relayAttempt{
		relayRequest: req,
		outAdapter:   outbound.Get(channel.Type),
		channel:      channel,
		usedKey:      channel.Keys[0],
	}

	statusCode, err := ra.forwardViaWS(context.Background())
	if err != nil {
		t.Fatalf("expected continuation ws request to recover by redial, got err %v", err)
	}
	if statusCode != http.StatusOK {
		t.Fatalf("expected continuation ws request to succeed after redial, got %d", statusCode)
	}
	if accepted.Load() < 2 {
		t.Fatalf("expected initial continuation attempt plus forced redial, got %d accepted connections", accepted.Load())
	}
	if req.metrics.WSRecovery == nil || *req.metrics.WSRecovery != model.RelayLogWSRecoveryReconnect {
		t.Fatalf("expected ws reconnect recovery to be recorded, got %#v", req.metrics.WSRecovery)
	}
	if !strings.Contains(writer.Body.String(), `"response.completed"`) {
		t.Fatalf("expected ws reconnect stream to complete, got %s", writer.Body.String())
	}
	wsUpstreamPool.Remove(newWSPoolKey(channel.ID, channel.Keys[0].ID, buildUpstreamWSHeaders(c.Request.Header, channel, channel.Keys[0].ChannelKey)))
}

func TestForwardFallsBackToHTTPWithWSDowngradeRecorded(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ctx := setupRelayTestDB(t)
	if err := op.SettingSetString(model.SettingKeyRelayWSUpgradeEnabled, "true"); err != nil {
		t.Fatalf("SettingSetString relay ws upgrade failed: %v", err)
	}

	httpServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.EqualFold(r.Header.Get("Upgrade"), "websocket") {
			w.WriteHeader(http.StatusUpgradeRequired)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"resp_http","object":"response","created":1,"model":"gpt-4o","output":[{"type":"message","role":"assistant","content":[{"type":"output_text","text":"ok"}]}],"usage":{"input_tokens":1,"input_tokens_details":{"cached_tokens":0},"output_tokens":1,"output_tokens_details":{"reasoning_tokens":0},"total_tokens":2}}`))
	}))
	defer httpServer.Close()

	channel := &model.Channel{
		Name:     "relay-ws-downgrade",
		Type:     outbound.OutboundTypeOpenAIResponse,
		Enabled:  true,
		BaseUrls: []model.BaseUrl{{URL: httpServer.URL + "/v1"}},
		Model:    "gpt-4o",
		Keys:     []model.ChannelKey{{Enabled: true, ChannelKey: "downgrade-key"}},
	}
	if err := op.ChannelCreate(channel, ctx); err != nil {
		t.Fatalf("ChannelCreate failed: %v", err)
	}

	writer := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(writer)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(`{"model":"gpt-4o","input":"hello"}`))
	c.Request.Header.Set("Content-Type", "application/json")
	internalReq := &transformerModel.InternalLLMRequest{Model: "gpt-4o", Stream: boolPtr(false), RawAPIFormat: transformerModel.APIFormatOpenAIResponse}
	req := &relayRequest{
		c:               c,
		inAdapter:       inbound.Get(inbound.InboundTypeOpenAIResponse),
		internalRequest: internalReq,
		metrics:         NewRelayMetrics(1, "gpt-4o", nil, internalReq),
		apiKeyID:        1,
		requestModel:    "gpt-4o",
	}
	ra := &relayAttempt{
		relayRequest: req,
		outAdapter:   outbound.Get(channel.Type),
		channel:      channel,
		usedKey:      channel.Keys[0],
	}

	statusCode, err := ra.forward()
	if err != nil {
		t.Fatalf("expected http downgrade path to succeed, got err %v", err)
	}
	if statusCode != http.StatusOK {
		t.Fatalf("expected downgrade request to succeed via http, got %d", statusCode)
	}
	if req.metrics.WSRecovery == nil || *req.metrics.WSRecovery != model.RelayLogWSRecoveryDowngrade {
		t.Fatalf("expected ws downgrade recovery to be recorded, got %#v", req.metrics.WSRecovery)
	}
}

func TestForwardViaWSPreservesClientUserAgentHeaders(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ctx := setupRelayTestDB(t)

	var seenUserAgent atomic.Pointer[string]
	var seenAcceptLanguage atomic.Pointer[string]
	wsServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ua := r.Header.Get("User-Agent")
		al := r.Header.Get("Accept-Language")
		seenUserAgent.Store(&ua)
		seenAcceptLanguage.Store(&al)

		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close(websocket.StatusNormalClosure, "")

		_, _, err = conn.Read(r.Context())
		if err != nil {
			return
		}

		_ = conn.Write(r.Context(), websocket.MessageText, []byte(`{"type":"response.created","response":{"id":"resp_header","model":"gpt-4o"}}`))
		_ = conn.Write(r.Context(), websocket.MessageText, []byte(`{"type":"response.output_text.delta","delta":"ok"}`))
		_ = conn.Write(r.Context(), websocket.MessageText, []byte(`{"type":"response.completed","response":{"id":"resp_header","model":"gpt-4o","status":"completed","usage":{"input_tokens":1,"output_tokens":1,"total_tokens":2}}}`))
	}))
	defer wsServer.Close()

	channel := &model.Channel{
		Name:     "relay-ws-header-forward",
		Type:     outbound.OutboundTypeOpenAIResponse,
		Enabled:  true,
		BaseUrls: []model.BaseUrl{{URL: wsServer.URL + "/v1"}},
		Model:    "gpt-4o",
		Keys:     []model.ChannelKey{{Enabled: true, ChannelKey: "header-key"}},
	}
	if err := op.ChannelCreate(channel, ctx); err != nil {
		t.Fatalf("ChannelCreate failed: %v", err)
	}

	writer := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(writer)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	c.Request.Header.Set("Accept-Language", "zh-CN,zh;q=0.9")
	internalReq := &transformerModel.InternalLLMRequest{Model: "gpt-4o", Stream: boolPtr(true)}
	req := &relayRequest{
		c:               c,
		inAdapter:       inbound.Get(inbound.InboundTypeOpenAIResponse),
		internalRequest: internalReq,
		metrics:         NewRelayMetrics(1, "gpt-4o", nil, internalReq),
		apiKeyID:        1,
		requestModel:    "gpt-4o",
	}
	ra := &relayAttempt{
		relayRequest: req,
		outAdapter:   outbound.Get(channel.Type),
		channel:      channel,
		usedKey:      channel.Keys[0],
	}

	statusCode, err := ra.forwardViaWS(context.Background())
	if err != nil {
		t.Fatalf("expected ws request to succeed, got err %v", err)
	}
	if statusCode != http.StatusOK {
		t.Fatalf("expected ws request to succeed, got %d", statusCode)
	}

	if got := seenUserAgent.Load(); got == nil || *got != "" {
		t.Fatalf("expected upstream ws handshake to omit user-agent when client does not send one, got %#v", got)
	}
	if got := seenAcceptLanguage.Load(); got == nil || *got != "zh-CN,zh;q=0.9" {
		t.Fatalf("expected accept-language to be forwarded, got %#v", got)
	}

	wsUpstreamPool.Remove(newWSPoolKey(channel.ID, channel.Keys[0].ID, buildUpstreamWSHeaders(c.Request.Header, channel, channel.Keys[0].ChannelKey)))
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

func TestHandleResponsesCompactProxiesSuccessfulResponse(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ctx := setupRelayTestDB(t)

	var hits atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		if r.URL.Path != "/v1/responses/compact" {
			http.Error(w, `{"error":"unexpected path"}`, http.StatusNotFound)
			return
		}
		if got := r.Header.Get("Authorization"); got != "Bearer compact-key" {
			http.Error(w, fmt.Sprintf(`{"error":"unexpected auth %q"}`, got), http.StatusUnauthorized)
			return
		}
		body, _ := io.ReadAll(r.Body)
		if !strings.Contains(string(body), `"previous_response_id":"resp_123"`) {
			http.Error(w, `{"error":"missing previous_response_id"}`, http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"resp_cmp_1","object":"response.compaction","created_at":1764967971,"output":[{"id":"cmp_001","type":"compaction","encrypted_content":"secret"}],"usage":{"input_tokens":12,"input_tokens_details":{"cached_tokens":3},"output_tokens":4,"output_tokens_details":{"reasoning_tokens":1},"total_tokens":16}}`))
	}))
	defer server.Close()

	channel := &model.Channel{
		Name:     "relay-compact-openai",
		Type:     outbound.OutboundTypeOpenAIResponse,
		Enabled:  true,
		BaseUrls: []model.BaseUrl{{URL: server.URL + "/v1"}},
		Model:    "compact-model",
		Keys:     []model.ChannelKey{{Enabled: true, ChannelKey: "compact-key"}},
	}
	if err := op.ChannelCreate(channel, ctx); err != nil {
		t.Fatalf("ChannelCreate failed: %v", err)
	}

	group := &model.Group{Name: "relay-compact-group", Mode: model.GroupModeFailover}
	if err := op.GroupCreate(group, ctx); err != nil {
		t.Fatalf("GroupCreate failed: %v", err)
	}
	if err := op.GroupItemAdd(&model.GroupItem{GroupID: group.ID, ChannelID: channel.ID, ModelName: "compact-model", Priority: 1, Weight: 1}, ctx); err != nil {
		t.Fatalf("GroupItemAdd failed: %v", err)
	}

	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Set("api_key_id", 42)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses/compact", strings.NewReader(`{"model":"relay-compact-group","previous_response_id":"resp_123"}`))
	c.Request.Header.Set("Content-Type", "application/json")

	HandleResponsesCompact(c)

	if recorder.Code != http.StatusOK {
		t.Fatalf("expected compact proxy to succeed, got status %d body %s", recorder.Code, recorder.Body.String())
	}
	if hits.Load() != 1 {
		t.Fatalf("expected exactly one upstream compact request, got %d", hits.Load())
	}
	if !strings.Contains(recorder.Body.String(), `"object":"response.compaction"`) {
		t.Fatalf("expected compact response to be proxied, got %s", recorder.Body.String())
	}
	if sticky := balancer.GetSticky(42, "relay-compact-group", time.Minute); sticky == nil || sticky.ChannelID != channel.ID {
		t.Fatalf("expected compact success to refresh sticky channel, got %#v", sticky)
	}
	logItems, err := op.RelayLogList(ctx, nil, nil, nil, 1, 10)
	if err != nil {
		t.Fatalf("RelayLogList failed: %v", err)
	}
	if len(logItems) == 0 {
		t.Fatalf("expected compact request to be logged")
	}
	if logItems[0].InputTokens != 12 || logItems[0].OutputTokens != 4 {
		t.Fatalf("expected compact usage to be logged, got input=%d output=%d", logItems[0].InputTokens, logItems[0].OutputTokens)
	}
}

func TestHandleResponsesCompactSkipsIncompatibleChannels(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ctx := setupRelayTestDB(t)

	var hits atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"resp_cmp_2","object":"response.compaction","created_at":1,"output":[],"usage":{"input_tokens":1,"input_tokens_details":{"cached_tokens":0},"output_tokens":1,"output_tokens_details":{"reasoning_tokens":0},"total_tokens":2}}`))
	}))
	defer server.Close()

	chatChannel := &model.Channel{
		Name:     "relay-compact-chat-only",
		Type:     outbound.OutboundTypeOpenAIChat,
		Enabled:  true,
		BaseUrls: []model.BaseUrl{{URL: server.URL + "/v1"}},
		Model:    "compact-model",
		Keys:     []model.ChannelKey{{Enabled: true, ChannelKey: "chat-key"}},
	}
	if err := op.ChannelCreate(chatChannel, ctx); err != nil {
		t.Fatalf("ChannelCreate chat channel failed: %v", err)
	}

	responseChannel := &model.Channel{
		Name:     "relay-compact-response",
		Type:     outbound.OutboundTypeOpenAIResponse,
		Enabled:  true,
		BaseUrls: []model.BaseUrl{{URL: server.URL + "/v1"}},
		Model:    "compact-model",
		Keys:     []model.ChannelKey{{Enabled: true, ChannelKey: "response-key"}},
	}
	if err := op.ChannelCreate(responseChannel, ctx); err != nil {
		t.Fatalf("ChannelCreate response channel failed: %v", err)
	}

	group := &model.Group{Name: "relay-compact-mixed-group", Mode: model.GroupModeFailover}
	if err := op.GroupCreate(group, ctx); err != nil {
		t.Fatalf("GroupCreate failed: %v", err)
	}
	if err := op.GroupItemAdd(&model.GroupItem{GroupID: group.ID, ChannelID: chatChannel.ID, ModelName: "compact-model", Priority: 1, Weight: 1}, ctx); err != nil {
		t.Fatalf("GroupItemAdd chat item failed: %v", err)
	}
	if err := op.GroupItemAdd(&model.GroupItem{GroupID: group.ID, ChannelID: responseChannel.ID, ModelName: "compact-model", Priority: 2, Weight: 1}, ctx); err != nil {
		t.Fatalf("GroupItemAdd response item failed: %v", err)
	}

	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses/compact", strings.NewReader(`{"model":"relay-compact-mixed-group","input":[{"role":"user","content":"hello"}]}`))
	c.Request.Header.Set("Content-Type", "application/json")

	HandleResponsesCompact(c)

	if recorder.Code != http.StatusOK {
		t.Fatalf("expected compact proxy to skip chat-only channel and succeed, got status %d body %s", recorder.Code, recorder.Body.String())
	}
	if hits.Load() != 1 {
		t.Fatalf("expected only the compatible response channel to be called, got %d hits", hits.Load())
	}
	logs, err := op.RelayLogList(ctx, nil, nil, nil, 1, 10)
	if err != nil {
		t.Fatalf("RelayLogList failed: %v", err)
	}
	if len(logs) == 0 || len(logs[0].Attempts) < 2 {
		t.Fatalf("expected relay attempts to include skipped incompatible channel, got %#v", logs)
	}
	if logs[0].Attempts[0].Status != model.AttemptSkipped {
		t.Fatalf("expected first attempt to skip incompatible channel, got %#v", logs[0].Attempts[0])
	}
}

func setupRelayTestDB(t *testing.T) context.Context {
	t.Helper()

	if dbpkg.GetDB() != nil {
		_ = dbpkg.Close()
	}
	balancer.Reset()
	resetWSConversationStateStore()
	resetWSUpstreamPool()

	dbPath := filepath.Join(t.TempDir(), "octopus-relay-test.db")
	if err := dbpkg.InitDB("sqlite", dbPath, false); err != nil {
		t.Fatalf("InitDB failed: %v", err)
	}
	if err := op.InitCache(); err != nil {
		t.Fatalf("InitCache failed: %v", err)
	}
	t.Cleanup(func() {
		balancer.Reset()
		resetWSConversationStateStore()
		resetWSUpstreamPool()
		_ = dbpkg.Close()
	})

	return context.Background()
}
