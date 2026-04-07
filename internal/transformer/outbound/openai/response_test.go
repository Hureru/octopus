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

func TestTransformStreamAggregatesFunctionCallIDAcrossEvents(t *testing.T) {
	outbound := &ResponseOutbound{}

	first, err := outbound.TransformStream(nil, []byte(`{"type":"response.output_item.added","output_index":0,"item":{"type":"function_call","call_id":"call_123","name":"lookup"}}`))
	if err != nil {
		t.Fatalf("first function call stream transform failed: %v", err)
	}
	if first == nil || len(first.Choices) != 1 || first.Choices[0].Delta == nil {
		t.Fatalf("expected initial function call delta, got %#v", first)
	}
	if got := first.Choices[0].Delta.ToolCalls[0].ID; got != "call_123" {
		t.Fatalf("expected initial function call id to be preserved, got %q", got)
	}

	second, err := outbound.TransformStream(nil, []byte(`{"type":"response.function_call_arguments.delta","output_index":0,"call_id":"call_123","name":"lookup","delta":"{}"}`))
	if err != nil {
		t.Fatalf("second function call stream transform failed: %v", err)
	}
	if second == nil || len(second.Choices) != 1 || second.Choices[0].Delta == nil {
		t.Fatalf("expected function call arguments delta, got %#v", second)
	}
	toolCall := second.Choices[0].Delta.ToolCalls[0]
	if toolCall.ID != "call_123" {
		t.Fatalf("expected function call id to survive argument delta, got %q", toolCall.ID)
	}
	if toolCall.Function.Arguments != "{}" {
		t.Fatalf("expected function call arguments delta to be preserved, got %q", toolCall.Function.Arguments)
	}
}

func stringPtr(value string) *string {
	return &value
}
