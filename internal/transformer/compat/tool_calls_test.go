package compat

import (
	"testing"

	"github.com/bestruirui/octopus/internal/transformer/model"
)

func TestFixOrphanedToolCallsInsertsMissingResults(t *testing.T) {
	followup := "next"
	messages := []model.Message{
		{
			Role: "assistant",
			ToolCalls: []model.ToolCall{
				{ID: "call_a", Function: model.FunctionCall{Name: "lookup"}},
				{ID: "call_b", Function: model.FunctionCall{Name: "search"}},
			},
		},
		{
			Role:       "tool",
			ToolCallID: stringPtr("call_a"),
			Content:    model.MessageContent{Content: stringPtr("ok")},
		},
		{Role: "user", Content: model.MessageContent{Content: &followup}},
	}

	got := FixOrphanedToolCalls(messages)
	if len(got) != 4 {
		t.Fatalf("expected one synthetic tool result, got %d messages: %+v", len(got), got)
	}
	if got[1].Role != "tool" || got[1].ToolCallID == nil || *got[1].ToolCallID != "call_b" {
		t.Fatalf("unexpected synthetic tool result: %+v", got[1])
	}
	if got[1].Content.Content == nil || *got[1].Content.Content != "" {
		t.Fatalf("expected empty synthetic content, got %+v", got[1].Content)
	}
	if got[2].Role != "tool" || got[2].ToolCallID == nil || *got[2].ToolCallID != "call_a" {
		t.Fatalf("existing tool result was not preserved after synthetic result: %+v", got[2])
	}
}

func TestFixOrphanedToolCallsStopsAtNextAssistant(t *testing.T) {
	messages := []model.Message{
		{
			Role:      "assistant",
			ToolCalls: []model.ToolCall{{ID: "call_a", Function: model.FunctionCall{Name: "lookup"}}},
		},
		{
			Role:      "assistant",
			ToolCalls: []model.ToolCall{{ID: "call_b", Function: model.FunctionCall{Name: "search"}}},
		},
		{
			Role:       "tool",
			ToolCallID: stringPtr("call_a"),
		},
	}

	got := FixOrphanedToolCalls(messages)
	if len(got) != 5 {
		t.Fatalf("expected both assistant turns to be patched independently, got %+v", got)
	}
	if got[1].ToolCallID == nil || *got[1].ToolCallID != "call_a" {
		t.Fatalf("first assistant was not patched before next assistant: %+v", got)
	}
	if got[3].ToolCallID == nil || *got[3].ToolCallID != "call_b" {
		t.Fatalf("second assistant was not patched: %+v", got)
	}
}

func stringPtr(v string) *string {
	return &v
}
