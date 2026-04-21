package anthropic

import (
	"context"
	"encoding/json"
	"testing"

	anthropicModel "github.com/bestruirui/octopus/internal/transformer/inbound/anthropic"
)

func TestStreamUsageAggregatesCacheTokens(t *testing.T) {
	outbound := &MessageOutbound{}

	startUsage := &anthropicModel.Usage{
		InputTokens:              120,
		CacheReadInputTokens:     500,
		CacheCreationInputTokens: 30,
	}
	msgStart := anthropicModel.StreamEvent{
		Type: "message_start",
		Message: &anthropicModel.StreamMessage{
			ID:    "msg_01",
			Type:  "message",
			Model: "claude-3-5-sonnet",
			Usage: startUsage,
		},
	}
	startBytes, err := json.Marshal(msgStart)
	if err != nil {
		t.Fatalf("marshal message_start: %v", err)
	}
	if _, err := outbound.TransformStream(context.Background(), startBytes); err != nil {
		t.Fatalf("message_start transform error: %v", err)
	}

	stopReason := "end_turn"
	delta := anthropicModel.StreamEvent{
		Type:  "message_delta",
		Delta: &anthropicModel.StreamDelta{StopReason: &stopReason},
		Usage: &anthropicModel.Usage{OutputTokens: 80},
	}
	deltaBytes, err := json.Marshal(delta)
	if err != nil {
		t.Fatalf("marshal message_delta: %v", err)
	}
	if _, err := outbound.TransformStream(context.Background(), deltaBytes); err != nil {
		t.Fatalf("message_delta transform error: %v", err)
	}

	stop := anthropicModel.StreamEvent{Type: "message_stop"}
	stopBytes, err := json.Marshal(stop)
	if err != nil {
		t.Fatalf("marshal message_stop: %v", err)
	}
	resp, err := outbound.TransformStream(context.Background(), stopBytes)
	if err != nil {
		t.Fatalf("message_stop transform error: %v", err)
	}
	if resp == nil || resp.Usage == nil {
		t.Fatalf("expected usage on message_stop, got %+v", resp)
	}

	u := resp.Usage
	if u.PromptTokens != 120 {
		t.Fatalf("PromptTokens = %d, want 120", u.PromptTokens)
	}
	if u.CacheReadInputTokens != 500 {
		t.Fatalf("CacheReadInputTokens = %d, want 500", u.CacheReadInputTokens)
	}
	if u.CacheCreationInputTokens != 30 {
		t.Fatalf("CacheCreationInputTokens = %d, want 30", u.CacheCreationInputTokens)
	}
	if u.CompletionTokens != 80 {
		t.Fatalf("CompletionTokens = %d, want 80", u.CompletionTokens)
	}
	if want := int64(120 + 500 + 30 + 80); u.TotalTokens != want {
		t.Fatalf("TotalTokens = %d, want %d (input+cache_read+cache_write+output)", u.TotalTokens, want)
	}
	if !u.HasAnthropicCacheSemantic() {
		t.Fatalf("expected anthropic cache semantic to be detected")
	}
	if got := u.EffectiveInputTokens(); got != int64(120+500+30) {
		t.Fatalf("EffectiveInputTokens = %d, want 650", got)
	}
}
