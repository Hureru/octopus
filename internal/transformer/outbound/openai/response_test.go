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

func TestConvertToResponsesRequestSanitizesRawReasoningInputSummary(t *testing.T) {
	req := &model.InternalLLMRequest{
		Model: "gpt-4o",
		RawInputItems: json.RawMessage(`[
			{"type":"input_text","text":"hello"},
			{"type":"reasoning","encrypted_content":"enc","native_meta":{"keep":true}}
		]`),
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

	if len(payload.Input) != 2 {
		t.Fatalf("expected two raw input items, got %#v", payload.Input)
	}

	reasoning := payload.Input[1]
	summary, ok := reasoning["summary"].([]any)
	if !ok || len(summary) != 1 {
		t.Fatalf("expected reasoning summary to be added, got %#v", reasoning["summary"])
	}
	part, ok := summary[0].(map[string]any)
	if !ok || part["type"] != "summary_text" || part["text"] != "" {
		t.Fatalf("expected default summary_text part, got %#v", summary[0])
	}
	if _, ok := reasoning["native_meta"]; !ok {
		t.Fatalf("expected native fields to be preserved, got %#v", reasoning)
	}
	if reasoning["encrypted_content"] != "enc" {
		t.Fatalf("expected encrypted_content to be preserved, got %#v", reasoning["encrypted_content"])
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

func TestConvertToResponsesRequestOmitsDeprecatedUser(t *testing.T) {
	user := "legacy-user"
	req := &model.InternalLLMRequest{
		Model:    "gpt-4o",
		User:     &user,
		Metadata: map[string]string{"trace_id": "abc123"},
		Messages: []model.Message{{
			Role: "user",
			Content: model.MessageContent{
				Content: stringPtr("hello"),
			},
		}},
	}

	responsesReq := ConvertToResponsesRequest(req)
	body, err := json.Marshal(responsesReq)
	if err != nil {
		t.Fatalf("marshal responses request failed: %v", err)
	}

	var payload map[string]any
	if err := json.Unmarshal(body, &payload); err != nil {
		t.Fatalf("unmarshal responses request failed: %v", err)
	}

	if _, ok := payload["user"]; ok {
		t.Fatalf("expected deprecated user to be omitted, got %#v", payload["user"])
	}
	if _, ok := payload["metadata"]; !ok {
		t.Fatalf("expected metadata to remain available, got %#v", payload)
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

	completed, err := outbound.TransformStream(nil, []byte(`{"type":"response.completed","response":{"id":"resp_123","model":"gpt-4o","status":"completed"}}`))
	if err != nil {
		t.Fatalf("completed stream transform failed: %v", err)
	}
	if completed == nil || len(completed.RawResponsesOutputItems) == 0 {
		t.Fatalf("expected completed stream response to preserve exact output items, got %#v", completed)
	}
}

func TestConvertToLLMResponseFromResponsesPreservesRawOutputItems(t *testing.T) {
	resp := &ResponsesResponse{
		ID:        "resp_123",
		Object:    "response",
		Model:     "gpt-4o",
		CreatedAt: 1,
		Output: []ResponsesItem{{
			Type:      "function_call",
			CallID:    "call_123",
			Name:      "lookup",
			Arguments: `{}`,
		}},
	}

	internalResp := convertToLLMResponseFromResponses(resp)
	if len(internalResp.RawResponsesOutputItems) == 0 {
		t.Fatalf("expected raw responses output items to be preserved")
	}
	var items []map[string]any
	if err := json.Unmarshal(internalResp.RawResponsesOutputItems, &items); err != nil {
		t.Fatalf("unmarshal raw output items failed: %v", err)
	}
	if len(items) != 1 || items[0]["type"] != "function_call" {
		t.Fatalf("expected original output items to be kept, got %#v", items)
	}
}

func stringPtr(value string) *string {
	return &value
}
