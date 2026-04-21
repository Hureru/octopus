package openai

import (
	"context"
	"testing"

	"github.com/bestruirui/octopus/internal/transformer/model"
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

// O-H7: streaming aggregator must merge Chat delta.Audio across chunks
// (gpt-5-audio and other audio-capable models stream incremental
// data/transcript while id stays stable). Previously the field was
// ignored during aggregation and the resulting message carried no
// Audio payload.
func TestChatInboundAggregatesAudioDelta(t *testing.T) {
	inbound := &ChatInbound{}
	ctx := context.Background()

	chunk := func(id string, data, transcript string, exp int64) *model.InternalLLMResponse {
		return &model.InternalLLMResponse{
			ID: "resp-1",
			Choices: []model.Choice{{
				Index: 0,
				Delta: &model.Message{
					Audio: &struct {
						Data       string `json:"data,omitempty"`
						ExpiresAt  int64  `json:"expires_at,omitempty"`
						ID         string `json:"id,omitempty"`
						Transcript string `json:"transcript,omitempty"`
					}{
						ID:         id,
						Data:       data,
						Transcript: transcript,
						ExpiresAt:  exp,
					},
				},
			}},
		}
	}

	if _, err := inbound.TransformStream(ctx, chunk("aud-1", "AAA", "Hello", 1700000000)); err != nil {
		t.Fatalf("TransformStream 1: %v", err)
	}
	if _, err := inbound.TransformStream(ctx, chunk("", "BBB", " world", 0)); err != nil {
		t.Fatalf("TransformStream 2: %v", err)
	}

	result, err := inbound.GetInternalResponse(ctx)
	if err != nil {
		t.Fatalf("GetInternalResponse: %v", err)
	}
	if result == nil || len(result.Choices) != 1 {
		t.Fatalf("expected 1 choice, got %+v", result)
	}
	msg := result.Choices[0].Message
	if msg == nil || msg.Audio == nil {
		t.Fatalf("expected aggregated audio, got %+v", msg)
	}
	if msg.Audio.ID != "aud-1" {
		t.Errorf("expected ID carried from first chunk, got %q", msg.Audio.ID)
	}
	if msg.Audio.Data != "AAABBB" {
		t.Errorf("expected data concatenated, got %q", msg.Audio.Data)
	}
	if msg.Audio.Transcript != "Hello world" {
		t.Errorf("expected transcript concatenated, got %q", msg.Audio.Transcript)
	}
	if msg.Audio.ExpiresAt != 1700000000 {
		t.Errorf("expected expires_at preserved, got %d", msg.Audio.ExpiresAt)
	}
}

// O-H2: Chat inbound must tag the internal request with
// APIFormatOpenAIChatCompletion so downstream transformers can tell Chat
// requests apart from Responses requests.
func TestChatInboundTagsRawAPIFormat(t *testing.T) {
	body := []byte(`{
		"model": "gpt-5",
		"messages": [{"role":"user","content":"hi"}]
	}`)

	inbound := &ChatInbound{}
	req, err := inbound.TransformRequest(context.Background(), body)
	if err != nil {
		t.Fatalf("TransformRequest err = %v", err)
	}
	if req.RawAPIFormat != model.APIFormatOpenAIChatCompletion {
		t.Fatalf("expected RawAPIFormat=%q, got %q",
			model.APIFormatOpenAIChatCompletion, req.RawAPIFormat)
	}
}
