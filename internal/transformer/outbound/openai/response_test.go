package openai

import (
	"encoding/json"
	"testing"

	"github.com/bestruirui/octopus/internal/transformer/model"
)

func TestConvertToResponsesRequestPreservesRawInputItems(t *testing.T) {
	req := &model.InternalLLMRequest{
		Model:         "gpt-4o",
		RawInputItems: json.RawMessage(`[{"type":"input_text","text":"raw","native_meta":{"keep":true}}]`),
		Messages: []model.Message{{
			Role: "user",
			Content: model.MessageContent{
				Content: stringPtr("normalized"),
			},
		}},
	}

	responsesReq := ConvertToResponsesRequest(req)
	body, err := json.Marshal(responsesReq)
	if err != nil {
		t.Fatalf("marshal responses request failed: %v", err)
	}

	var payload struct {
		Input []map[string]any `json:"input"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		t.Fatalf("unmarshal responses request failed: %v", err)
	}
	if len(payload.Input) != 1 {
		t.Fatalf("expected raw input item to be preserved, got %#v", payload.Input)
	}
	if payload.Input[0]["text"] != "raw" {
		t.Fatalf("expected raw input text to be preserved, got %#v", payload.Input[0])
	}
	if _, ok := payload.Input[0]["native_meta"]; !ok {
		t.Fatalf("expected native field to be preserved, got %#v", payload.Input[0])
	}
	if payload.Input[0]["text"] == "normalized" {
		t.Fatalf("expected raw input items to take precedence over normalized messages")
	}
}

func TestMarshalResponsesInputItemsBuildsArrayInput(t *testing.T) {
	data, err := MarshalResponsesInputItems([]model.Message{{
		Role: "assistant",
		ToolCalls: []model.ToolCall{{
			ID:   "call_123",
			Type: "function",
			Function: model.FunctionCall{
				Name:      "lookup",
				Arguments: `{}`,
			},
		}},
	}})
	if err != nil {
		t.Fatalf("marshal responses input items failed: %v", err)
	}

	var items []map[string]any
	if err := json.Unmarshal(data, &items); err != nil {
		t.Fatalf("unmarshal marshaled items failed: %v", err)
	}
	if len(items) != 1 || items[0]["type"] != "function_call" {
		t.Fatalf("expected assistant tool call to become function_call item, got %#v", items)
	}
}

func stringPtr(value string) *string {
	return &value
}
