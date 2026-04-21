package openai

import (
	"encoding/json"
	"testing"
)

func TestConvertToInternalRequestPreservesRawInputItems(t *testing.T) {
	req := &ResponsesRequest{
		Model: "gpt-4o",
		Input: ResponsesInput{Items: []ResponsesItem{
			{Type: "input_text", Text: stringPtr("hello")},
		}},
	}

	internalReq, err := convertToInternalRequest(req)
	if err != nil {
		t.Fatalf("convertToInternalRequest failed: %v", err)
	}
	if len(internalReq.RawInputItems) == 0 {
		t.Fatalf("expected raw input items to be preserved")
	}

	var items []map[string]any
	if err := json.Unmarshal(internalReq.RawInputItems, &items); err != nil {
		t.Fatalf("unmarshal raw input items failed: %v", err)
	}
	if len(items) != 1 || items[0]["type"] != "input_text" {
		t.Fatalf("expected original raw input items to be kept, got %#v", items)
	}
	if internalReq.TransformOptions.ArrayInputs == nil || !*internalReq.TransformOptions.ArrayInputs {
		t.Fatalf("expected array input flag to stay true")
	}
}

func TestConvertToInternalRequestMarksPassthroughForUnsupportedToolType(t *testing.T) {
	req := &ResponsesRequest{
		Model: "gpt-4o",
		Input: ResponsesInput{Text: stringPtr("hello")},
		Tools: []ResponsesTool{{
			Type: "apply_patch",
		}},
	}

	internalReq, err := convertToInternalRequest(req)
	if err != nil {
		t.Fatalf("convertToInternalRequest failed: %v", err)
	}
	if !internalReq.RequiresOpenAIResponsesPassthrough() {
		t.Fatalf("expected unsupported responses tool to require passthrough")
	}
}

func TestConvertToInternalRequestMarksPassthroughForUnsupportedInputItem(t *testing.T) {
	req := &ResponsesRequest{
		Model: "gpt-4o",
		Input: ResponsesInput{Items: []ResponsesItem{{
			Type:   "apply_patch_call_output",
			CallID: "apc_123",
		}}},
	}

	internalReq, err := convertToInternalRequest(req)
	if err != nil {
		t.Fatalf("convertToInternalRequest failed: %v", err)
	}
	if !internalReq.RequiresOpenAIResponsesPassthrough() {
		t.Fatalf("expected unsupported responses input item to require passthrough")
	}
}

func stringPtr(value string) *string {
	return &value
}
