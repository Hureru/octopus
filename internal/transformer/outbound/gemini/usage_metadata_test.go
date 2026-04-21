package gemini

import (
	"testing"

	"github.com/bestruirui/octopus/internal/transformer/model"
)

func TestConvertGeminiToLLMResponseCarriesUsageMetadataDetails(t *testing.T) {
	geminiResp := &model.GeminiGenerateContentResponse{
		UsageMetadata: &model.GeminiUsageMetadata{
			PromptTokenCount:        1000,
			CandidatesTokenCount:    200,
			TotalTokenCount:         1200,
			CachedContentTokenCount: 400,
			ThoughtsTokenCount:      50,
			ToolUsePromptTokenCount: 75,
			PromptTokensDetails: []model.GeminiModalityTokenCount{
				{Modality: "TEXT", TokenCount: 800},
				{Modality: "IMAGE", TokenCount: 200},
			},
			CandidatesTokensDetails: []model.GeminiModalityTokenCount{
				{Modality: "TEXT", TokenCount: 200},
			},
		},
	}

	resp := convertGeminiToLLMResponse(geminiResp, false, nil)
	if resp.Usage == nil {
		t.Fatalf("expected usage to be populated")
	}

	u := resp.Usage
	if u.PromptTokens != 1000 {
		t.Fatalf("PromptTokens = %d, want 1000", u.PromptTokens)
	}
	if u.ToolUsePromptTokens != 75 {
		t.Fatalf("ToolUsePromptTokens = %d, want 75", u.ToolUsePromptTokens)
	}
	if u.PromptTokensDetails == nil {
		t.Fatalf("PromptTokensDetails should be set")
	}
	if u.PromptTokensDetails.CachedTokens != 400 {
		t.Fatalf("CachedTokens = %d, want 400", u.PromptTokensDetails.CachedTokens)
	}
	if u.PromptTokensDetails.TextTokens != 800 {
		t.Fatalf("TextTokens = %d, want 800", u.PromptTokensDetails.TextTokens)
	}
	if u.PromptTokensDetails.ImageTokens != 200 {
		t.Fatalf("ImageTokens = %d, want 200", u.PromptTokensDetails.ImageTokens)
	}
	if u.CompletionTokensDetails == nil || u.CompletionTokensDetails.ReasoningTokens != 50 {
		t.Fatalf("ReasoningTokens not set, got %+v", u.CompletionTokensDetails)
	}
	if u.CompletionTokensDetails.TextTokens != 200 {
		t.Fatalf("Completion TextTokens = %d, want 200", u.CompletionTokensDetails.TextTokens)
	}
	if len(u.PromptModalityTokenDetails) != 2 {
		t.Fatalf("PromptModalityTokenDetails len = %d, want 2", len(u.PromptModalityTokenDetails))
	}
}
