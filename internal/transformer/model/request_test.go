package model

import (
	"encoding/json"
	"testing"
)

func requestTestStringPtr(value string) *string {
	return &value
}

func TestInternalLLMRequestValidateFillsStableToolCallIDs(t *testing.T) {
	makeRequest := func(prefix bool) *InternalLLMRequest {
		messages := []Message{{
			Role: "assistant",
			ToolCalls: []ToolCall{{
				Type: "function",
				Function: FunctionCall{
					Name:      "lookup",
					Arguments: `{"q":"octopus","limit":1}`,
				},
			}},
		}}
		if prefix {
			messages = append([]Message{{Role: "user", Content: MessageContent{Content: requestTestStringPtr("prefix")}}}, messages...)
		}
		return &InternalLLMRequest{Model: "gpt-4o", Messages: messages}
	}

	first := makeRequest(false)
	second := makeRequest(true)
	if err := first.Validate(); err != nil {
		t.Fatalf("validate first: %v", err)
	}
	if err := second.Validate(); err != nil {
		t.Fatalf("validate second: %v", err)
	}
	firstID := first.Messages[0].ToolCalls[0].ID
	secondID := second.Messages[1].ToolCalls[0].ID
	if firstID == "" || secondID == "" {
		t.Fatalf("expected generated IDs, got %q and %q", firstID, secondID)
	}
	if firstID != secondID {
		t.Fatalf("expected ID independent of message index, got %q and %q", firstID, secondID)
	}
}

func TestInternalLLMRequestValidatePreservesExistingToolCallIDs(t *testing.T) {
	req := &InternalLLMRequest{
		Model: "gpt-4o",
		Messages: []Message{{
			Role: "assistant",
			ToolCalls: []ToolCall{{
				ID:   "call_existing",
				Type: "function",
				Function: FunctionCall{
					Name:      "lookup",
					Arguments: `{}`,
				},
			}},
		}},
	}
	if err := req.Validate(); err != nil {
		t.Fatalf("validate: %v", err)
	}
	if got := req.Messages[0].ToolCalls[0].ID; got != "call_existing" {
		t.Fatalf("expected existing ID preserved, got %q", got)
	}
}

func TestInternalLLMRequestValidateDisambiguatesDuplicateGeneratedToolCallIDs(t *testing.T) {
	req := &InternalLLMRequest{
		Model: "gpt-4o",
		Messages: []Message{{
			Role: "assistant",
			ToolCalls: []ToolCall{
				{Type: "function", Function: FunctionCall{Name: "lookup", Arguments: `{"q":"octopus"}`}},
				{Type: "function", Function: FunctionCall{Name: "lookup", Arguments: `{"q":"octopus"}`}},
			},
		}},
	}
	if err := req.Validate(); err != nil {
		t.Fatalf("validate: %v", err)
	}
	first := req.Messages[0].ToolCalls[0].ID
	second := req.Messages[0].ToolCalls[1].ID
	if first == "" || second == "" || first == second {
		t.Fatalf("expected unique generated IDs, got %q and %q", first, second)
	}
}

func TestInternalLLMRequestValidateAllowsResponsesRawInputItems(t *testing.T) {
	req := &InternalLLMRequest{
		Model:        "gpt-4o",
		RawAPIFormat: APIFormatOpenAIResponse,
		RawInputItems: json.RawMessage(`[
			{"type":"computer_call","id":"call_1"}
		]`),
	}

	if err := req.Validate(); err != nil {
		t.Fatalf("expected raw responses input items to satisfy validation, got %v", err)
	}
	if !req.IsChatRequest() {
		t.Fatalf("expected raw responses input items to be treated as chat request")
	}
}
