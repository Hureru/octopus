package anthropic

import (
	"context"
	"testing"
)

func TestTransformRequestMapsAnthropicUserIDToUser(t *testing.T) {
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
	if req.User == nil || *req.User != "user-123" {
		t.Fatalf("expected user to be preserved, got %+v", req.User)
	}
	if got := req.TransformerMetadata["anthropic_user_id"]; got != "user-123" {
		t.Fatalf("expected transformer metadata to keep anthropic user id, got %q", got)
	}
	if req.Metadata["user_id"] != "" {
		t.Fatalf("expected generic metadata.user_id to stay empty, got %q", req.Metadata["user_id"])
	}
}
