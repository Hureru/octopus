package openai

import (
	"encoding/json"
	"testing"

	"github.com/bestruirui/octopus/internal/transformer/model"
	"github.com/samber/lo"
)

func TestConvertInputFromMessagesGeneratesFunctionCallIDAndItemReference(t *testing.T) {
	// Test that function_call items get unique IDs and function_call_output items get item_reference
	msgs := []model.Message{
		{
			Role: "assistant",
			ToolCalls: []model.ToolCall{
				{
					ID:   "call_abc123",
					Type: "function",
					Function: model.FunctionCall{
						Name:      "get_weather",
						Arguments: `{"location":"Beijing"}`,
					},
				},
			},
		},
		{
			Role:       "tool",
			ToolCallID: lo.ToPtr("call_abc123"),
			Content: model.MessageContent{
				Content: lo.ToPtr("Sunny, 25°C"),
			},
		},
	}

	input := convertInputFromMessages(msgs, model.TransformOptions{ArrayInputs: lo.ToPtr(true)})

	if len(input.Items) != 2 {
		t.Fatalf("expected 2 items, got %d", len(input.Items))
	}

	// Check function_call has ID
	functionCall := input.Items[0]
	if functionCall.Type != "function_call" {
		t.Fatalf("expected first item to be function_call, got %s", functionCall.Type)
	}
	if functionCall.ID == "" {
		t.Error("function_call item missing ID")
	}
	if functionCall.CallID != "call_abc123" {
		t.Errorf("expected call_id=call_abc123, got %s", functionCall.CallID)
	}

	// Check function_call_output has item_reference
	functionCallOutput := input.Items[1]
	if functionCallOutput.Type != "function_call_output" {
		t.Fatalf("expected second item to be function_call_output, got %s", functionCallOutput.Type)
	}
	if functionCallOutput.ItemReference == nil {
		t.Fatal("function_call_output item missing item_reference")
	}
	if *functionCallOutput.ItemReference != functionCall.ID {
		t.Errorf("item_reference=%s doesn't match function_call ID=%s", *functionCallOutput.ItemReference, functionCall.ID)
	}
}

func TestSanitizeResponsesRawItemsAddsItemReference(t *testing.T) {
	// Test that sanitizeResponsesRawItems automatically adds missing item_reference
	rawItems := json.RawMessage(`[
		{
			"id": "item_xyz789",
			"type": "function_call",
			"call_id": "call_abc123",
			"name": "get_weather",
			"arguments": "{\"location\":\"Beijing\"}"
		},
		{
			"type": "function_call_output",
			"call_id": "call_abc123",
			"output": {"text": "Sunny, 25°C"}
		}
	]`)

	sanitized := sanitizeResponsesRawItems(rawItems)

	var items []map[string]interface{}
	if err := json.Unmarshal(sanitized, &items); err != nil {
		t.Fatalf("failed to unmarshal sanitized items: %v", err)
	}

	if len(items) != 2 {
		t.Fatalf("expected 2 items, got %d", len(items))
	}

	// Check function_call_output now has item_reference
	functionCallOutput := items[1]
	if functionCallOutput["type"] != "function_call_output" {
		t.Fatalf("expected second item to be function_call_output, got %v", functionCallOutput["type"])
	}

	itemRef, ok := functionCallOutput["item_reference"].(string)
	if !ok {
		t.Fatal("function_call_output missing item_reference after sanitization")
	}
	if itemRef != "item_xyz789" {
		t.Errorf("expected item_reference=item_xyz789, got %s", itemRef)
	}
}

func TestSanitizeResponsesRawItemsFixesNullItemReference(t *testing.T) {
	tests := []struct {
		name string
		raw  string
	}{
		{"null value", `[
			{"id":"item_xyz","type":"function_call","call_id":"call_1","name":"f","arguments":"{}"},
			{"type":"function_call_output","call_id":"call_1","item_reference":null,"output":{"text":"ok"}}
		]`},
		{"empty string", `[
			{"id":"item_xyz","type":"function_call","call_id":"call_1","name":"f","arguments":"{}"},
			{"type":"function_call_output","call_id":"call_1","item_reference":"","output":{"text":"ok"}}
		]`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sanitized := sanitizeResponsesRawItems(json.RawMessage(tt.raw))
			var items []map[string]interface{}
			if err := json.Unmarshal(sanitized, &items); err != nil {
				t.Fatalf("unmarshal: %v", err)
			}
			ref, ok := items[1]["item_reference"].(string)
			if !ok || ref != "item_xyz" {
				t.Errorf("expected item_reference=item_xyz, got %v", items[1]["item_reference"])
			}
		})
	}
}

func TestSanitizeResponsesRawItemsBackfillsMissingFunctionCallID(t *testing.T) {
	rawItems := json.RawMessage(`[
		{
			"type": "function_call",
			"call_id": "call_noid",
			"name": "do_thing",
			"arguments": "{}"
		},
		{
			"type": "function_call_output",
			"call_id": "call_noid",
			"output": {"text": "done"}
		}
	]`)

	sanitized := sanitizeResponsesRawItems(rawItems)

	var items []map[string]interface{}
	if err := json.Unmarshal(sanitized, &items); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	generatedID, ok := items[0]["id"].(string)
	if !ok || generatedID == "" {
		t.Fatal("function_call missing generated id")
	}

	ref, ok := items[1]["item_reference"].(string)
	if !ok || ref == "" {
		t.Fatal("function_call_output missing item_reference")
	}
	if ref != generatedID {
		t.Errorf("item_reference=%s doesn't match generated id=%s", ref, generatedID)
	}
}

func TestMarshalResponsesInputItemsPreservesItemReference(t *testing.T) {
	// Test end-to-end: Messages -> Items -> JSON preserves item_reference
	msgs := []model.Message{
		{
			Role: "assistant",
			ToolCalls: []model.ToolCall{
				{
					ID:   "call_test123",
					Type: "function",
					Function: model.FunctionCall{
						Name:      "test_func",
						Arguments: `{}`,
					},
				},
			},
		},
		{
			Role:       "tool",
			ToolCallID: lo.ToPtr("call_test123"),
			Content: model.MessageContent{
				Content: lo.ToPtr("result"),
			},
		},
	}

	rawItems, err := MarshalResponsesInputItems(msgs)
	if err != nil {
		t.Fatalf("MarshalResponsesInputItems failed: %v", err)
	}

	var items []map[string]interface{}
	if err := json.Unmarshal(rawItems, &items); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}

	// Find function_call and function_call_output, then verify item_reference matches function_call.id
	var functionCallID string
	var itemReference string
	var foundCall, foundOutput bool
	for _, item := range items {
		switch item["type"] {
		case "function_call":
			if id, ok := item["id"].(string); ok {
				functionCallID = id
				foundCall = true
			}
		case "function_call_output":
			if ref, ok := item["item_reference"].(string); ok {
				itemReference = ref
				foundOutput = true
			}
		}
	}

	if !foundCall {
		t.Fatal("function_call item not found in marshaled output")
	}
	if functionCallID == "" {
		t.Fatal("function_call item has empty id")
	}
	if !foundOutput {
		t.Fatal("function_call_output item missing item_reference")
	}
	if itemReference != functionCallID {
		t.Errorf("item_reference=%s doesn't match function_call id=%s", itemReference, functionCallID)
	}
}
