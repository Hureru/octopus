package anthropic

import (
	"context"
	"strings"
	"testing"

	"github.com/bestruirui/octopus/internal/transformer/model"
)

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
