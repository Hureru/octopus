package anthropic

import (
	"context"
	"encoding/json"
	"io"
	"testing"

	"github.com/bestruirui/octopus/internal/transformer/model"
)

func TestTransformRequestRawRewritesModel(t *testing.T) {
	outbound := &MessageOutbound{}
	rawBody := []byte(`{
		"model":"internal-alias",
		"max_tokens":16,
		"messages":[{"role":"user","content":"hello"}],
		"metadata":{"user_id":"user-123"},
		"custom_flag":true
	}`)

	req, err := outbound.TransformRequestRaw(
		context.Background(),
		rawBody,
		"claude-3-5-sonnet-20241022",
		"https://example.com/v1",
		"test-key",
		nil,
	)
	if err != nil {
		t.Fatalf("TransformRequestRaw() error = %v", err)
	}

	body, err := io.ReadAll(req.Body)
	if err != nil {
		t.Fatalf("ReadAll(req.Body) error = %v", err)
	}

	var payload map[string]any
	if err := json.Unmarshal(body, &payload); err != nil {
		t.Fatalf("unmarshal rewritten body error = %v", err)
	}
	if got := payload["model"]; got != "claude-3-5-sonnet-20241022" {
		t.Fatalf("expected rewritten model, got %#v", got)
	}
	if got := payload["custom_flag"]; got != true {
		t.Fatalf("expected custom fields to survive rewrite, got %#v", got)
	}
}

func TestConvertToAnthropicRequestUsesUserFallbackForMetadata(t *testing.T) {
	req := &model.InternalLLMRequest{
		Model: "claude-3-5-sonnet",
		User:  stringPtr("user-456"),
		Messages: []model.Message{
			{
				Role: "user",
				Content: model.MessageContent{
					Content: stringPtr("hello"),
				},
			},
		},
	}

	anthropicReq := convertToAnthropicRequest(req)
	if anthropicReq.Metadata == nil || anthropicReq.Metadata.UserID != "user-456" {
		t.Fatalf("expected anthropic metadata user_id to use internal user fallback, got %+v", anthropicReq.Metadata)
	}
}

func stringPtr(v string) *string {
	return &v
}
