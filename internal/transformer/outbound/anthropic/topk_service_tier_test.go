package anthropic

import (
	"testing"

	"github.com/samber/lo"

	anthropicModel "github.com/bestruirui/octopus/internal/transformer/inbound/anthropic"
	"github.com/bestruirui/octopus/internal/transformer/model"
)

// TestConvertToAnthropicRequestForwardsTopKAndServiceTier covers A-H3: the
// internal TopK / ServiceTier fields must be written back onto the outbound
// Anthropic request so that clients setting `top_k` or `service_tier` keep
// getting their value applied upstream.
func TestConvertToAnthropicRequestForwardsTopKAndServiceTier(t *testing.T) {
	tier := "priority"
	req := &model.InternalLLMRequest{
		Model:       "claude-3-5-sonnet",
		Temperature: lo.ToPtr(0.3),
		TopP:        lo.ToPtr(0.9),
		TopK:        lo.ToPtr[int64](40),
		ServiceTier: &tier,
		Messages: []model.Message{
			{Role: "user", Content: model.MessageContent{Content: stringPtr("hi")}},
		},
	}

	out := convertToAnthropicRequest(req)
	if out.TopK == nil || *out.TopK != 40 {
		t.Fatalf("expected top_k forwarded, got %+v", out.TopK)
	}
	if out.ServiceTier != "priority" {
		t.Fatalf("expected service_tier forwarded, got %q", out.ServiceTier)
	}
}

// TestApplyThinkingParamConstraintsEnabled covers A-H4: when extended thinking
// is active, Anthropic requires temperature == 1 and forbids top_p / top_k.
func TestApplyThinkingParamConstraintsEnabled(t *testing.T) {
	req := &model.InternalLLMRequest{
		Model:           "claude-3-7-sonnet",
		Temperature:     lo.ToPtr(0.4),
		TopP:            lo.ToPtr(0.95),
		TopK:            lo.ToPtr[int64](40),
		ReasoningEffort: "high",
		Messages: []model.Message{
			{Role: "user", Content: model.MessageContent{Content: stringPtr("hi")}},
		},
	}

	out := convertToAnthropicRequest(req)
	if out.Thinking == nil || out.Thinking.Type != anthropicModel.ThinkingTypeEnabled {
		t.Fatalf("expected thinking enabled, got %+v", out.Thinking)
	}
	if out.Temperature == nil || *out.Temperature != 1.0 {
		t.Fatalf("expected temperature normalised to 1.0, got %+v", out.Temperature)
	}
	if out.TopP != nil {
		t.Fatalf("expected top_p cleared, got %+v", out.TopP)
	}
	if out.TopK != nil {
		t.Fatalf("expected top_k cleared, got %+v", out.TopK)
	}
}

// TestApplyThinkingParamConstraintsAdaptive: adaptive thinking has the same
// sampling-parameter restrictions as enabled thinking.
func TestApplyThinkingParamConstraintsAdaptive(t *testing.T) {
	req := &model.InternalLLMRequest{
		Model:            "claude-3-7-sonnet",
		Temperature:      lo.ToPtr(0.7),
		TopP:             lo.ToPtr(0.9),
		TopK:             lo.ToPtr[int64](50),
		ReasoningEffort:  "medium",
		AdaptiveThinking: true,
		Messages: []model.Message{
			{Role: "user", Content: model.MessageContent{Content: stringPtr("hi")}},
		},
	}

	out := convertToAnthropicRequest(req)
	if out.Thinking == nil || out.Thinking.Type != anthropicModel.ThinkingTypeAdaptive {
		t.Fatalf("expected adaptive thinking, got %+v", out.Thinking)
	}
	if out.Temperature == nil || *out.Temperature != 1.0 {
		t.Fatalf("expected adaptive to force temperature 1.0, got %+v", out.Temperature)
	}
	if out.TopP != nil || out.TopK != nil {
		t.Fatalf("expected adaptive to clear top_p/top_k, got top_p=%v top_k=%v", out.TopP, out.TopK)
	}
}

// TestApplyThinkingParamConstraintsNoThinking: without thinking, sampling
// knobs must survive untouched — sanity guard so the helper does not
// over-reach.
func TestApplyThinkingParamConstraintsNoThinking(t *testing.T) {
	req := &model.InternalLLMRequest{
		Model:       "claude-3-5-sonnet",
		Temperature: lo.ToPtr(0.4),
		TopP:        lo.ToPtr(0.9),
		TopK:        lo.ToPtr[int64](40),
		Messages: []model.Message{
			{Role: "user", Content: model.MessageContent{Content: stringPtr("hi")}},
		},
	}

	out := convertToAnthropicRequest(req)
	if out.Thinking != nil {
		t.Fatalf("expected no thinking, got %+v", out.Thinking)
	}
	if out.Temperature == nil || *out.Temperature != 0.4 {
		t.Fatalf("expected temperature preserved, got %+v", out.Temperature)
	}
	if out.TopP == nil || *out.TopP != 0.9 {
		t.Fatalf("expected top_p preserved, got %+v", out.TopP)
	}
	if out.TopK == nil || *out.TopK != 40 {
		t.Fatalf("expected top_k preserved, got %+v", out.TopK)
	}
}
