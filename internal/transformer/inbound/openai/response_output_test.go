package openai

import (
	"testing"

	"github.com/samber/lo"

	"github.com/bestruirui/octopus/internal/transformer/model"
)

func TestStreamCompletedEventHasNonEmptyOutputWithMessage(t *testing.T) {
	text := "hello"
	stop := "stop"
	chunks := []*model.InternalLLMResponse{
		{
			ID:     "resp_01",
			Model:  "gpt-4o",
			Object: "chat.completion.chunk",
			Choices: []model.Choice{{
				Index: 0,
				Delta: &model.Message{
					Role:    "assistant",
					Content: model.MessageContent{Content: &text},
				},
			}},
		},
		{
			ID:     "resp_01",
			Model:  "gpt-4o",
			Object: "chat.completion.chunk",
			Choices: []model.Choice{{
				Index:        0,
				Delta:        &model.Message{Role: "assistant"},
				FinishReason: &stop,
			}},
		},
		{
			ID:     "resp_01",
			Model:  "gpt-4o",
			Object: "chat.completion.chunk",
			Usage: &model.Usage{
				PromptTokens:     1,
				CompletionTokens: 1,
				TotalTokens:      2,
			},
		},
	}
	events := feedStream(t, chunks)

	var completed *ResponsesStreamEvent
	for i := range events {
		if events[i].Type == "response.completed" {
			completed = &events[i]
			break
		}
	}
	if completed == nil || completed.Response == nil {
		t.Fatalf("expected response.completed event, got events: %+v", eventTypes(events))
	}
	if len(completed.Response.Output) == 0 {
		t.Fatalf("response.completed.output must be non-empty (O-H3)")
	}
	first := completed.Response.Output[0]
	if first.Type != "message" {
		t.Fatalf("first output type = %q, want message", first.Type)
	}
}

func TestStreamCompletedSynthesizesShellWhenEmpty(t *testing.T) {
	stop := "stop"
	chunks := []*model.InternalLLMResponse{
		{
			ID:     "resp_02",
			Model:  "gpt-4o",
			Object: "chat.completion.chunk",
			Choices: []model.Choice{{
				Index:        0,
				Delta:        &model.Message{Role: "assistant"},
				FinishReason: &stop,
			}},
		},
		{
			ID:    "resp_02",
			Model: "gpt-4o",
			Usage: &model.Usage{
				PromptTokens:     0,
				CompletionTokens: 0,
				TotalTokens:      0,
			},
		},
	}
	events := feedStream(t, chunks)

	var completed *ResponsesStreamEvent
	for i := range events {
		if events[i].Type == "response.completed" {
			completed = &events[i]
		}
	}
	if completed == nil || completed.Response == nil {
		t.Fatalf("expected response.completed event")
	}
	if len(completed.Response.Output) == 0 {
		t.Fatalf("output must be non-empty even when no items were emitted")
	}
	first := completed.Response.Output[0]
	if first.Type != "message" {
		t.Fatalf("synthetic output type = %q, want message", first.Type)
	}
	if first.Status == nil || *first.Status != "completed" {
		t.Fatalf("synthetic status = %v, want completed", first.Status)
	}
	_ = lo.ToPtr("ignore")
}

func TestConvertToResponsesAPIResponsePreservesRefusalContent(t *testing.T) {
	stop := "refusal"
	resp := &model.InternalLLMResponse{
		ID:      "resp_refusal",
		Model:   "gpt-4o",
		Created: 123,
		Choices: []model.Choice{{
			Message: &model.Message{
				Role:    "assistant",
				Refusal: "I cannot help with that.",
			},
			FinishReason: &stop,
		}},
	}

	out := convertToResponsesAPIResponse(resp)
	if len(out.Output) != 1 {
		t.Fatalf("expected 1 output item, got %d", len(out.Output))
	}
	msg := out.Output[0]
	if msg.Type != "message" || msg.Content == nil || len(msg.Content.Items) != 1 {
		t.Fatalf("unexpected message shape: %+v", msg)
	}
	part := msg.Content.Items[0]
	if part.Type != "refusal" || part.Refusal == nil || *part.Refusal != "I cannot help with that." {
		t.Fatalf("expected refusal content item, got %+v", part)
	}
	if out.Status == nil || *out.Status != "failed" {
		t.Fatalf("expected failed status for refusal stop, got %v", out.Status)
	}
}
