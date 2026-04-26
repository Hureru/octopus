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

func TestChatInboundAggregatesStreamWithModelAggregator(t *testing.T) {
	inbound := &ChatInbound{}
	ctx := context.Background()
	text1 := "hel"
	text2 := "lo"
	finish := model.FinishReasonStop.String()

	if _, err := inbound.TransformStream(ctx, &model.InternalLLMResponse{Object: "[DONE]"}); err != nil {
		t.Fatalf("TransformStream done: %v", err)
	}
	if result, err := inbound.GetInternalResponse(ctx); err != nil || result != nil {
		t.Fatalf("expected done chunk not to aggregate, got result=%#v err=%v", result, err)
	}
	if _, err := inbound.TransformStream(ctx, &model.InternalLLMResponse{
		ID: "chunk-1",
		Choices: []model.Choice{{
			Index: 0,
			Delta: &model.Message{
				Role:    "assistant",
				Content: model.MessageContent{Content: &text1},
				ToolCalls: []model.ToolCall{{
					ID:    "call_1",
					Type:  "function",
					Index: 0,
					Function: model.FunctionCall{
						Name:      "look",
						Arguments: `{"q":`,
					},
				}},
			},
		}},
	}); err != nil {
		t.Fatalf("TransformStream first: %v", err)
	}
	if _, err := inbound.TransformStream(ctx, &model.InternalLLMResponse{
		ID: "chunk-2",
		Choices: []model.Choice{{
			Index: 0,
			Delta: &model.Message{
				Content: model.MessageContent{Content: &text2},
				ToolCalls: []model.ToolCall{{
					Index: 0,
					Function: model.FunctionCall{
						Name:      "up",
						Arguments: `"octopus"}`,
					},
				}},
			},
			FinishReason: &finish,
		}},
	}); err != nil {
		t.Fatalf("TransformStream second: %v", err)
	}

	result, err := inbound.GetInternalResponse(ctx)
	if err != nil {
		t.Fatalf("GetInternalResponse: %v", err)
	}
	if result == nil || len(result.Choices) != 1 || result.Choices[0].Message == nil {
		t.Fatalf("unexpected result: %#v", result)
	}
	message := result.Choices[0].Message
	if message.Content.Content == nil || *message.Content.Content != "hello" {
		t.Fatalf("unexpected content: %#v", message.Content.Content)
	}
	if len(message.ToolCalls) != 1 || message.ToolCalls[0].Function.Name != "lookup" || message.ToolCalls[0].Function.Arguments != `{"q":"octopus"}` {
		t.Fatalf("unexpected tool calls: %#v", message.ToolCalls)
	}
	if result.Choices[0].FinishReason == nil || *result.Choices[0].FinishReason != finish {
		t.Fatalf("unexpected finish reason: %#v", result.Choices[0].FinishReason)
	}
	if second, err := inbound.GetInternalResponse(ctx); err != nil || second != nil {
		t.Fatalf("expected aggregator reset, got result=%#v err=%v", second, err)
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
