package openai

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/samber/lo"

	"github.com/bestruirui/octopus/internal/transformer/model"
)

func TestTruncationRoundTripNonStream(t *testing.T) {
	i := &ResponseInbound{}
	req := ResponsesRequest{
		Model:      "gpt-4o",
		Input:      ResponsesInput{Text: lo.ToPtr("hi")},
		Truncation: lo.ToPtr("auto"),
	}
	body, _ := json.Marshal(req)
	internal, err := i.TransformRequest(context.Background(), body)
	if err != nil {
		t.Fatalf("TransformRequest error: %v", err)
	}
	if internal.Truncation == nil || *internal.Truncation != "auto" {
		t.Fatalf("internal Truncation = %v, want auto", internal.Truncation)
	}

	internalResp := &model.InternalLLMResponse{
		ID:     "resp_1",
		Model:  "gpt-4o",
		Object: "chat.completion",
		Choices: []model.Choice{{
			Index:        0,
			Message:      &model.Message{Role: "assistant", Content: model.MessageContent{Content: lo.ToPtr("done")}},
			FinishReason: lo.ToPtr("stop"),
		}},
	}
	out, err := i.TransformResponse(context.Background(), internalResp)
	if err != nil {
		t.Fatalf("TransformResponse error: %v", err)
	}
	var parsed ResponsesResponse
	if err := json.Unmarshal(out, &parsed); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if parsed.Truncation == nil || *parsed.Truncation != "auto" {
		t.Fatalf("response Truncation = %v, want auto", parsed.Truncation)
	}
}

func TestTruncationRoundTripStream(t *testing.T) {
	i := &ResponseInbound{}
	req := ResponsesRequest{
		Model:      "gpt-4o",
		Input:      ResponsesInput{Text: lo.ToPtr("hi")},
		Truncation: lo.ToPtr("disabled"),
	}
	body, _ := json.Marshal(req)
	if _, err := i.TransformRequest(context.Background(), body); err != nil {
		t.Fatalf("TransformRequest error: %v", err)
	}

	text := "hello"
	stop := "stop"
	chunks := []*model.InternalLLMResponse{
		{
			ID:     "resp_1",
			Model:  "gpt-4o",
			Object: "chat.completion.chunk",
			Choices: []model.Choice{{
				Index:        0,
				Delta:        &model.Message{Role: "assistant", Content: model.MessageContent{Content: &text}},
				FinishReason: &stop,
			}},
		},
		{
			ID:    "resp_1",
			Model: "gpt-4o",
			Usage: &model.Usage{PromptTokens: 1, CompletionTokens: 1, TotalTokens: 2},
		},
	}

	ctx := context.Background()
	var raw []byte
	for _, c := range chunks {
		out, err := i.TransformStream(ctx, c)
		if err != nil {
			t.Fatalf("TransformStream error: %v", err)
		}
		raw = append(raw, out...)
	}
	events := parseSSEEvents(t, raw)

	found := false
	for _, ev := range events {
		if ev.Type == "response.created" || ev.Type == "response.in_progress" || ev.Type == "response.completed" {
			if ev.Response != nil && ev.Response.Truncation != nil && *ev.Response.Truncation == "disabled" {
				found = true
				break
			}
		}
	}
	if !found {
		t.Fatalf("expected truncation=disabled to be echoed in a stream event; events=%v", eventTypes(events))
	}
}
