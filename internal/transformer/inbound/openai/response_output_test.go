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
