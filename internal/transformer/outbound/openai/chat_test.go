package openai

import (
	"encoding/json"
	"testing"

	"github.com/bestruirui/octopus/internal/transformer/model"
)

func TestBuildChatCompletionsRequestUsesExplicitWhitelist(t *testing.T) {
	content := "hello"
	user := "legacy-user"
	safetyID := "safe-user"
	enableThinking := true

	req := &model.InternalLLMRequest{
		Model: "gpt-4o",
		Messages: []model.Message{{
			Role: "developer",
			Content: model.MessageContent{
				Content: &content,
			},
		}},
		User:                    &user,
		SafetyIdentifier:        &safetyID,
		EnableThinking:          &enableThinking,
		Metadata:                map[string]string{"trace_id": "abc123"},
		ResponsesPromptCacheKey: stringPtr("resp_cache_only"),
		Audio: &struct {
			Format string `json:"format,omitempty"`
			Voice  string `json:"voice,omitempty"`
		}{
			Format: "mp3",
			Voice:  "alloy",
		},
	}

	wire := buildChatCompletionsRequest(req)
	body, err := json.Marshal(wire)
	if err != nil {
		t.Fatalf("marshal chat request failed: %v", err)
	}

	var payload map[string]any
	if err := json.Unmarshal(body, &payload); err != nil {
		t.Fatalf("unmarshal chat request failed: %v", err)
	}

	if got := payload["model"]; got != "gpt-4o" {
		t.Fatalf("expected model to be preserved, got %#v", got)
	}
	if got := payload["safety_identifier"]; got != safetyID {
		t.Fatalf("expected safety_identifier to be preserved, got %#v", got)
	}
	if _, ok := payload["metadata"]; !ok {
		t.Fatalf("expected metadata to be preserved, got %#v", payload)
	}
	if _, ok := payload["user"]; ok {
		t.Fatalf("expected deprecated user to be omitted, got %#v", payload["user"])
	}
	if _, ok := payload["enable_thinking"]; ok {
		t.Fatalf("expected provider-specific enable_thinking to be omitted, got %#v", payload["enable_thinking"])
	}
	if _, ok := payload["prompt_cache_key"]; ok {
		t.Fatalf("expected responses-only prompt_cache_key to be omitted, got %#v", payload["prompt_cache_key"])
	}

	audio, ok := payload["audio"].(map[string]any)
	if !ok || audio["format"] != "mp3" || audio["voice"] != "alloy" {
		t.Fatalf("expected audio settings to be preserved, got %#v", payload["audio"])
	}
}

// TestBuildChatCompletionsRequestForwardsPromptCacheKey verifies that a
// client-supplied prompt_cache_key on the Chat entrypoint reaches the
// upstream Chat Completions payload. Before O-C4, PromptCacheKey was a
// *bool that json.Unmarshal never populated from a string wire value, so
// the cache key was silently dropped — losing up to ~90% input-cost savings
// for any client that relied on prompt-cache bucketing.
func TestBuildChatCompletionsRequestForwardsPromptCacheKey(t *testing.T) {
	cacheKey := "session-abc-123"
	content := "hi"
	req := &model.InternalLLMRequest{
		Model: "gpt-4o",
		Messages: []model.Message{{
			Role:    "user",
			Content: model.MessageContent{Content: &content},
		}},
		PromptCacheKey: &cacheKey,
	}

	wire := buildChatCompletionsRequest(req)
	body, err := json.Marshal(wire)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var payload map[string]any
	if err := json.Unmarshal(body, &payload); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got := payload["prompt_cache_key"]; got != cacheKey {
		t.Fatalf("expected prompt_cache_key=%q in chat payload, got %#v", cacheKey, got)
	}
}
