package openai

import (
	"context"
	"testing"
)

// O-H1: the inbound chat parser must populate the new 2025 Chat fields from
// the client wire JSON so the outbound whitelist has something to forward.
func TestChatInboundParses2025Fields(t *testing.T) {
	body := []byte(`{
		"model": "gpt-5",
		"messages": [{"role":"user","content":"hi"}],
		"verbosity": "medium",
		"prediction": {"type":"content","content":"abc"},
		"web_search_options": {"search_context_size":"medium"},
		"user": "u-1"
	}`)

	inbound := &ChatInbound{}
	req, err := inbound.TransformRequest(context.Background(), body)
	if err != nil {
		t.Fatalf("TransformRequest err = %v", err)
	}
	if req.Verbosity == nil || *req.Verbosity != "medium" {
		t.Fatalf("expected verbosity=medium, got %#v", req.Verbosity)
	}
	if len(req.Prediction) == 0 {
		t.Fatalf("expected prediction raw bytes to be preserved")
	}
	if len(req.WebSearchOptions) == 0 {
		t.Fatalf("expected web_search_options raw bytes to be preserved")
	}
	if req.User == nil || *req.User != "u-1" {
		t.Fatalf("expected user=u-1, got %#v", req.User)
	}
}
