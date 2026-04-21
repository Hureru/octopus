package gemini

import (
	"testing"

	"github.com/samber/lo"

	"github.com/bestruirui/octopus/internal/transformer/model"
)

// TestConvertLLMToGeminiRequestPopulatesNewConfigFields covers G-H1: the
// InternalLLMRequest penalty / logprob / seed knobs should land on the
// Gemini GenerationConfig and TopK should come from the native field in
// preference to the legacy metadata hook.
func TestConvertLLMToGeminiRequestPopulatesNewConfigFields(t *testing.T) {
	req := &model.InternalLLMRequest{
		Model:            "gemini-2.5-pro",
		Temperature:      lo.ToPtr(0.2),
		TopP:             lo.ToPtr(0.9),
		TopK:             lo.ToPtr[int64](32),
		PresencePenalty:  lo.ToPtr(0.6),
		FrequencyPenalty: lo.ToPtr(-0.3),
		Seed:             lo.ToPtr[int64](7),
		Logprobs:         lo.ToPtr(true),
		TopLogprobs:      lo.ToPtr[int64](3),
		TransformerMetadata: map[string]string{
			"gemini_top_k":            "64",
			"gemini_media_resolution": "MEDIA_RESOLUTION_HIGH",
		},
		Messages: []model.Message{
			{Role: "user", Content: model.MessageContent{Content: stringPtr("hi")}},
		},
	}

	gemReq := convertLLMToGeminiRequest(req)
	cfg := gemReq.GenerationConfig
	if cfg == nil {
		t.Fatalf("expected generation config, got nil")
	}
	if cfg.TopK == nil || *cfg.TopK != 32 {
		t.Fatalf("expected native TopK to win over metadata, got %+v", cfg.TopK)
	}
	if cfg.PresencePenalty == nil || *cfg.PresencePenalty != 0.6 {
		t.Fatalf("expected presencePenalty forwarded, got %+v", cfg.PresencePenalty)
	}
	if cfg.FrequencyPenalty == nil || *cfg.FrequencyPenalty != -0.3 {
		t.Fatalf("expected frequencyPenalty forwarded, got %+v", cfg.FrequencyPenalty)
	}
	if cfg.Seed == nil || *cfg.Seed != 7 {
		t.Fatalf("expected seed forwarded, got %+v", cfg.Seed)
	}
	if cfg.ResponseLogprobs == nil || !*cfg.ResponseLogprobs {
		t.Fatalf("expected responseLogprobs=true, got %+v", cfg.ResponseLogprobs)
	}
	if cfg.Logprobs == nil || *cfg.Logprobs != 3 {
		t.Fatalf("expected logprobs=3, got %+v", cfg.Logprobs)
	}
	if cfg.MediaResolution != "MEDIA_RESOLUTION_HIGH" {
		t.Fatalf("expected mediaResolution forwarded, got %q", cfg.MediaResolution)
	}
}

// Gemini caps logprobs at 5; requests asking for more should clamp.
func TestConvertLLMToGeminiRequestClampsLogprobs(t *testing.T) {
	req := &model.InternalLLMRequest{
		Model:       "gemini-2.5-flash",
		TopLogprobs: lo.ToPtr[int64](15),
		Messages: []model.Message{
			{Role: "user", Content: model.MessageContent{Content: stringPtr("hi")}},
		},
	}
	gemReq := convertLLMToGeminiRequest(req)
	if gemReq.GenerationConfig == nil || gemReq.GenerationConfig.Logprobs == nil {
		t.Fatalf("expected logprobs populated, got %+v", gemReq.GenerationConfig)
	}
	if *gemReq.GenerationConfig.Logprobs != 5 {
		t.Fatalf("expected logprobs clamped to 5, got %d", *gemReq.GenerationConfig.Logprobs)
	}
}

// Without TopK native field, legacy metadata hook still wins.
func TestConvertLLMToGeminiRequestFallsBackToLegacyTopKMetadata(t *testing.T) {
	req := &model.InternalLLMRequest{
		Model: "gemini-2.5-pro",
		TransformerMetadata: map[string]string{
			"gemini_top_k": "12",
		},
		Messages: []model.Message{
			{Role: "user", Content: model.MessageContent{Content: stringPtr("hi")}},
		},
	}
	gemReq := convertLLMToGeminiRequest(req)
	if gemReq.GenerationConfig == nil || gemReq.GenerationConfig.TopK == nil {
		t.Fatalf("expected legacy metadata TopK, got %+v", gemReq.GenerationConfig)
	}
	if *gemReq.GenerationConfig.TopK != 12 {
		t.Fatalf("expected legacy TopK=12, got %d", *gemReq.GenerationConfig.TopK)
	}
}
