package gemini

import (
	"testing"

	"github.com/bestruirui/octopus/internal/transformer/model"
)

func TestConvertGeminiToLLMResponseCarriesMetadata(t *testing.T) {
	geminiResp := &model.GeminiGenerateContentResponse{
		ResponseId:   "resp-42",
		ModelVersion: "gemini-2.5-pro-v20251007",
		CreateTime:   "2026-04-21T12:34:56Z",
		Candidates: []*model.GeminiCandidate{
			{
				Index: 0,
				Content: &model.GeminiContent{
					Role: "model",
					Parts: []*model.GeminiPart{
						{Text: "hi"},
					},
				},
			},
		},
	}

	resp := convertGeminiToLLMResponse(geminiResp, false, nil)
	if resp.ID != "resp-42" {
		t.Fatalf("ID = %q, want resp-42", resp.ID)
	}
	if resp.Model != "gemini-2.5-pro-v20251007" {
		t.Fatalf("Model = %q, want gemini-2.5-pro-v20251007", resp.Model)
	}
	if resp.Created == 0 {
		t.Fatalf("Created should be parsed from createTime, got 0")
	}
}

func TestConvertGeminiToLLMResponseSynthesizesBlockedChoice(t *testing.T) {
	geminiResp := &model.GeminiGenerateContentResponse{
		PromptFeedback: &model.GeminiPromptFeedback{
			BlockReason: "SAFETY",
		},
	}
	resp := convertGeminiToLLMResponse(geminiResp, false, nil)
	if len(resp.Choices) != 1 {
		t.Fatalf("expected synthesized choice for blocked prompt, got %d", len(resp.Choices))
	}
	fr := resp.Choices[0].FinishReason
	if fr == nil {
		t.Fatalf("FinishReason should be set on blocked prompt choice")
	}
	if got := *fr; got != string(model.FinishReasonSafety) && got != string(model.FinishReasonContentFilter) {
		t.Fatalf("FinishReason = %q, want safety-family or content_filter", got)
	}
}
