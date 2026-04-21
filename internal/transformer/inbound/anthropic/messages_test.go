package anthropic

import (
	"context"
	"strings"
	"testing"

	"github.com/bestruirui/octopus/internal/transformer/model"
)

// TestTransformRequestCapturesMCPServersAndContainer verifies A-H6:
// incoming mcp_servers / container payloads land on the internal request's
// Anthropic raw passthrough channels so outbound can replay them.
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
