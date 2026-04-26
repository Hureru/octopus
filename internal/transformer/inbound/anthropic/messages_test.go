package anthropic

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/bestruirui/octopus/internal/transformer/model"
)

// TestTransformRequestCapturesMCPServersAndContainer verifies A-H6:
// incoming mcp_servers / container payloads land on the internal request's
// Anthropic raw passthrough channels so outbound can replay them.
func TestAnthropicToolUseRoundTripsGeminiThoughtSignature(t *testing.T) {
	inbound := &MessagesInbound{}
	body := []byte(`{
		"model":"claude-3-5-sonnet",
		"max_tokens":16,
		"messages":[{
			"role":"assistant",
			"content":[{
				"type":"tool_use",
				"id":"call_Bash_2",
				"name":"Bash",
				"input":{"command":"pwd"},
				"_octopus":{"provider_extensions":{"gemini":{"thought_signature":"sig-gemini"}}}
			}]
		}]
	}`)

	req, err := inbound.TransformRequest(context.Background(), body)
	if err != nil {
		t.Fatalf("TransformRequest() error = %v", err)
	}
	if len(req.Messages) != 1 || len(req.Messages[0].ToolCalls) != 1 {
		t.Fatalf("expected one tool call, got %+v", req.Messages)
	}
	toolCall := req.Messages[0].ToolCalls[0]
	if toolCall.ThoughtSignature != "sig-gemini" {
		t.Fatalf("ThoughtSignature = %q, want sig-gemini", toolCall.ThoughtSignature)
	}
	if toolCall.ID != "call_Bash_2" || toolCall.Function.Name != "Bash" || toolCall.Function.Arguments != `{"command":"pwd"}` {
		t.Fatalf("tool call fields changed: %+v", toolCall)
	}
}

func TestTransformResponseEmitsGeminiThoughtSignatureExtension(t *testing.T) {
	inbound := &MessagesInbound{}
	out, err := inbound.TransformResponse(context.Background(), &model.InternalLLMResponse{
		ID:    "msg_1",
		Model: "gemini-3.1-pro",
		Choices: []model.Choice{{
			Message: &model.Message{
				Role: "assistant",
				ToolCalls: []model.ToolCall{{
					ID: "call_Bash_2",
					Function: model.FunctionCall{
						Name:      "Bash",
						Arguments: `{"command":"pwd"}`,
					},
					ThoughtSignature: "sig-gemini",
				}},
			},
		}},
	})
	if err != nil {
		t.Fatalf("TransformResponse() error = %v", err)
	}

	var resp Message
	if err := json.Unmarshal(out, &resp); err != nil {
		t.Fatalf("unmarshal response: %v\n%s", err, out)
	}
	if len(resp.Content) != 1 {
		t.Fatalf("expected one content block, got %+v", resp.Content)
	}
	got := geminiThoughtSignatureFromExtension(resp.Content[0].Octopus)
	if got != "sig-gemini" {
		t.Fatalf("extension signature = %q, want sig-gemini; raw=%s", got, out)
	}
}

func TestTransformResponseOmitsOctopusExtensionWithoutSignature(t *testing.T) {
	inbound := &MessagesInbound{}
	out, err := inbound.TransformResponse(context.Background(), &model.InternalLLMResponse{
		ID:    "msg_1",
		Model: "gemini-3.1-pro",
		Choices: []model.Choice{{
			Message: &model.Message{
				Role: "assistant",
				ToolCalls: []model.ToolCall{{
					ID: "call_Bash_2",
					Function: model.FunctionCall{
						Name:      "Bash",
						Arguments: `{}`,
					},
				}},
			},
		}},
	})
	if err != nil {
		t.Fatalf("TransformResponse() error = %v", err)
	}
	if strings.Contains(string(out), "_octopus") {
		t.Fatalf("unexpected _octopus extension without signature: %s", out)
	}
}

func TestTransformStreamEmitsGeminiThoughtSignatureExtension(t *testing.T) {
	inbound := &MessagesInbound{}
	out, err := inbound.TransformStream(context.Background(), &model.InternalLLMResponse{
		ID:     "msg_1",
		Model:  "gemini-3.1-pro",
		Object: "chat.completion.chunk",
		Choices: []model.Choice{{
			Delta: &model.Message{
				Role: "assistant",
				ToolCalls: []model.ToolCall{{
					Index: 2,
					ID:    "call_Bash_2",
					Function: model.FunctionCall{
						Name:      "Bash",
						Arguments: `{"command":"pwd"}`,
					},
					ThoughtSignature: "sig-gemini",
				}},
			},
		}},
	})
	if err != nil {
		t.Fatalf("TransformStream() error = %v", err)
	}
	text := string(out)
	if !strings.Contains(text, `"_octopus":{"provider_extensions":{"gemini":{"thought_signature":"sig-gemini"}}}`) {
		t.Fatalf("expected stream tool_use extension, got %s", text)
	}
}
func TestTransformRequestCapturesMCPServersAndContainer(t *testing.T) {
	inbound := &MessagesInbound{}
	body := []byte(`{
		"model":"claude-opus-4",
		"max_tokens":16,
		"messages":[{"role":"user","content":"hi"}],
		"mcp_servers":[{"type":"url","url":"https://example.invalid/mcp","name":"demo"}],
		"container":{"id":"cntr-1"}
	}`)
	req, err := inbound.TransformRequest(context.Background(), body)
	if err != nil {
		t.Fatalf("TransformRequest() error = %v", err)
	}
	if !strings.Contains(string(req.AnthropicMCPServers), "example.invalid/mcp") {
		t.Errorf("expected mcp_servers captured, got %s", req.AnthropicMCPServers)
	}
	if !strings.Contains(string(req.AnthropicContainer), "cntr-1") {
		t.Errorf("expected container captured, got %s", req.AnthropicContainer)
	}
}

func TestTransformRequestPreservesAnthropicUserIDInTransformerMetadataOnly(t *testing.T) {
	inbound := &MessagesInbound{}
	body := []byte(`{
		"model":"claude-3-5-sonnet",
		"max_tokens":16,
		"messages":[{"role":"user","content":"hello"}],
		"metadata":{"user_id":"user-123"}
	}`)

	req, err := inbound.TransformRequest(context.Background(), body)
	if err != nil {
		t.Fatalf("TransformRequest() error = %v", err)
	}
	if req.User != nil {
		t.Fatalf("expected user to remain unset for cross-provider safety, got %+v", req.User)
	}
	if got := req.TransformerMetadata["anthropic_user_id"]; got != "user-123" {
		t.Fatalf("expected transformer metadata to keep anthropic user id, got %q", got)
	}
	if req.Metadata["user_id"] != "" {
		t.Fatalf("expected generic metadata.user_id to stay empty, got %q", req.Metadata["user_id"])
	}
}

// A-H3: TransformRequest should surface Anthropic `top_k` and `service_tier`
// onto the internal request so outbound transformers (Anthropic, Gemini, and
// OpenAI-compat models such as Qwen) can forward them upstream.
func TestTransformRequestExtractsTopKAndServiceTier(t *testing.T) {
	inbound := &MessagesInbound{}
	body := []byte(`{
		"model":"claude-3-5-sonnet",
		"max_tokens":16,
		"messages":[{"role":"user","content":"hello"}],
		"top_k":32,
		"service_tier":"priority"
	}`)

	req, err := inbound.TransformRequest(context.Background(), body)
	if err != nil {
		t.Fatalf("TransformRequest: %v", err)
	}
	if req.TopK == nil || *req.TopK != 32 {
		t.Fatalf("expected top_k=32 on internal request, got %+v", req.TopK)
	}
	if req.ServiceTier == nil || *req.ServiceTier != "priority" {
		t.Fatalf("expected service_tier=priority on internal request, got %+v", req.ServiceTier)
	}
}

func TestTransformRequestPreservesUnknownCacheControlValues(t *testing.T) {
	inbound := &MessagesInbound{}
	body := []byte(`{
		"model":"claude-3-5-sonnet",
		"max_tokens":16,
		"messages":[{"role":"user","content":[{"type":"text","text":"hello","cache_control":{"type":"future_type","ttl":"future_ttl"}}]}]
	}`)

	req, err := inbound.TransformRequest(context.Background(), body)
	if err != nil {
		t.Fatalf("TransformRequest: %v", err)
	}
	if len(req.Messages) != 1 || req.Messages[0].CacheControl == nil {
		t.Fatalf("expected cache_control on simplified message, got %+v", req.Messages)
	}
	cc := req.Messages[0].CacheControl
	if cc.Type != "future_type" || cc.TTL != "future_ttl" {
		t.Fatalf("expected raw cache_control values preserved, got %+v", cc)
	}
}

func TestTransformStreamDoesNotStopMissingContentBlock(t *testing.T) {
	inbound := &MessagesInbound{}

	first, err := inbound.TransformStream(context.Background(), &model.InternalLLMResponse{
		ID:    "msg_1",
		Model: "gemini-3.1-pro",
		Choices: []model.Choice{
			{
				Index:        0,
				FinishReason: stringPtr("stop"),
			},
		},
	})
	if err != nil {
		t.Fatalf("first TransformStream() error = %v", err)
	}
	text := string(first)
	if strings.Contains(text, "content_block_stop") {
		t.Fatalf("expected no content_block_stop when no block was opened, got %s", text)
	}
	if strings.Contains(text, "message_stop") {
		t.Fatalf("expected message_stop to wait until usage or done, got %s", text)
	}

	done, err := inbound.TransformStream(context.Background(), &model.InternalLLMResponse{Object: "[DONE]"})
	if err != nil {
		t.Fatalf("done TransformStream() error = %v", err)
	}
	doneText := string(done)
	if !strings.Contains(doneText, "message_delta") || !strings.Contains(doneText, "message_stop") {
		t.Fatalf("expected done to finalize stream, got %s", doneText)
	}
}

func TestTransformStreamEventsDirectAnthropicSSE(t *testing.T) {
	inbound := &MessagesInbound{}
	events := []model.StreamEvent{
		{Kind: model.StreamEventKindMessageStart, ID: "msg_1", Model: "claude-test", Role: "assistant"},
		{Kind: model.StreamEventKindThinkingDelta, ID: "msg_1", Model: "claude-test", Delta: &model.StreamDelta{Thinking: "think", Signature: "sig"}},
		{Kind: model.StreamEventKindTextDelta, ID: "msg_1", Model: "claude-test", Delta: &model.StreamDelta{Text: "hello"}},
		{Kind: model.StreamEventKindToolCallStart, ID: "msg_1", Model: "claude-test", Index: 0, ToolCall: &model.ToolCall{Index: 0, ID: "call_1", Function: model.FunctionCall{Name: "lookup"}}},
		{Kind: model.StreamEventKindToolCallDelta, ID: "msg_1", Model: "claude-test", Index: 0, ToolCall: &model.ToolCall{Index: 0, ID: "call_1", Function: model.FunctionCall{Name: "lookup"}}, Delta: &model.StreamDelta{Arguments: `{"q":"x"}`}},
		{Kind: model.StreamEventKindMessageStop, ID: "msg_1", Model: "claude-test", StopReason: model.FinishReasonToolCalls},
		{Kind: model.StreamEventKindUsageDelta, ID: "msg_1", Model: "claude-test", Usage: &model.Usage{PromptTokens: 3, CompletionTokens: 4}},
	}

	out, err := inbound.TransformStreamEvents(context.Background(), events)
	if err != nil {
		t.Fatalf("TransformStreamEvents() error = %v", err)
	}
	text := string(out)
	for _, want := range []string{
		"event:message_start",
		`"type":"thinking_delta"`,
		`"type":"signature_delta"`,
		`"type":"text_delta"`,
		`"type":"tool_use"`,
		`"partial_json":"{\"q\":\"x\"}"`,
		`"stop_reason":"tool_use"`,
		`"input_tokens":3`,
		"event:message_stop",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("expected %q in SSE, got %s", want, text)
		}
	}
}

func TestTransformStreamEventsDirectErrorAndDone(t *testing.T) {
	inbound := &MessagesInbound{}
	errOut, err := inbound.TransformStreamEvents(context.Background(), []model.StreamEvent{{Kind: model.StreamEventKindError, Error: &model.ResponseError{Detail: model.ErrorDetail{Message: "boom"}}}})
	if err != nil {
		t.Fatalf("error event: %v", err)
	}
	if !strings.Contains(string(errOut), `"type":"api_error"`) || !strings.Contains(string(errOut), `"message":"boom"`) {
		t.Fatalf("expected api_error SSE, got %s", errOut)
	}

	inbound = &MessagesInbound{}
	doneOut, err := inbound.TransformStreamEvents(context.Background(), []model.StreamEvent{
		{Kind: model.StreamEventKindMessageStart, ID: "msg_1", Model: "claude-test"},
		{Kind: model.StreamEventKindTextDelta, Delta: &model.StreamDelta{Text: "hi"}},
		{Kind: model.StreamEventKindMessageStop, StopReason: model.FinishReasonStop},
		{Kind: model.StreamEventKindDone},
	})
	if err != nil {
		t.Fatalf("done event: %v", err)
	}
	if !strings.Contains(string(doneOut), "event:message_stop") {
		t.Fatalf("expected done to finalize message, got %s", doneOut)
	}
}

func stringPtr(v string) *string {
	return &v
}

// A-C2: when the outbound layer surfaces an upstream error chunk, the
// Anthropic inbound must emit an Anthropic-compatible `event: error` SSE frame
// so clients see the failure reason instead of a truncated response.
func TestTransformStreamSurfacesErrorAsSSE(t *testing.T) {
	inbound := &MessagesInbound{}

	out, err := inbound.TransformStream(context.Background(), &model.InternalLLMResponse{
		Error: &model.ResponseError{
			StatusCode: 529,
			Detail: model.ErrorDetail{
				Type:    "overloaded_error",
				Message: "Overloaded",
			},
		},
	})
	if err != nil {
		t.Fatalf("TransformStream() error = %v", err)
	}
	text := string(out)
	if !strings.Contains(text, "event:error") {
		t.Fatalf("expected `event:error` SSE frame, got %q", text)
	}
	if !strings.Contains(text, `"type":"overloaded_error"`) {
		t.Fatalf("expected error type to be preserved, got %q", text)
	}
	if !strings.Contains(text, `"message":"Overloaded"`) {
		t.Fatalf("expected error message to be preserved, got %q", text)
	}
}

// A-C2 (fallback): missing error.type should degrade to `api_error` so the
// Anthropic SSE payload remains schema-valid.
func TestTransformStreamErrorDefaultsTypeWhenEmpty(t *testing.T) {
	inbound := &MessagesInbound{}

	out, err := inbound.TransformStream(context.Background(), &model.InternalLLMResponse{
		Error: &model.ResponseError{
			Detail: model.ErrorDetail{Message: "unknown"},
		},
	})
	if err != nil {
		t.Fatalf("TransformStream() error = %v", err)
	}
	if !strings.Contains(string(out), `"type":"api_error"`) {
		t.Fatalf("expected fallback type=api_error, got %q", string(out))
	}
}

// A-H5: TransformRequest must preserve the server-tool Type and raw spec
// payload on the InternalLLMRequest so the outbound anthropic transformer can
// rehydrate the wire object and attach the matching beta header.
func TestTransformRequestPreservesServerToolOnInternalRequest(t *testing.T) {
	inbound := &MessagesInbound{}
	body := []byte(`{
		"model":"claude-3-5-sonnet",
		"max_tokens":16,
		"messages":[{"role":"user","content":"hello"}],
		"tools":[
			{"type":"web_search_20250305","name":"web_search","max_uses":3,"allowed_domains":["a.com"]},
			{"name":"lookup","description":"look","input_schema":{"type":"object"}}
		]
	}`)
	req, err := inbound.TransformRequest(context.Background(), body)
	if err != nil {
		t.Fatalf("TransformRequest: %v", err)
	}
	if len(req.Tools) != 2 {
		t.Fatalf("expected 2 tools, got %d", len(req.Tools))
	}
	srv := req.Tools[0]
	if srv.Type != "web_search_20250305" {
		t.Fatalf("server tool Type lost, got %q", srv.Type)
	}
	if len(srv.AnthropicServerSpec) == 0 {
		t.Fatalf("server tool raw spec lost")
	}
	if !strings.Contains(string(srv.AnthropicServerSpec), "allowed_domains") {
		t.Fatalf("spec-specific fields missing, got %s", string(srv.AnthropicServerSpec))
	}
	fn := req.Tools[1]
	if fn.Type != "function" {
		t.Fatalf("function tool Type = %q, want function", fn.Type)
	}
	if fn.Function.Name != "lookup" {
		t.Fatalf("function tool name mismatch, got %q", fn.Function.Name)
	}
}
