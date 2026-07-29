package volcengine

import (
	"encoding/json"
	"testing"

	"github.com/bestruirui/octopus/internal/transformer/outbound/openai"
)

func TestConvertToResponsesInputHandlesEmptyItems(t *testing.T) {
	input, err := convertToResponsesInput(openai.ResponsesInput{})
	if err != nil {
		t.Fatalf("convert empty input: %v", err)
	}
	payload, err := json.Marshal(input)
	if err != nil {
		t.Fatalf("marshal empty input: %v", err)
	}
	if string(payload) != "[]" {
		t.Fatalf("empty input payload = %s, want []", payload)
	}
}

func TestConvertToResponsesInputPreservesRawItemsAndMarksAssistantPartial(t *testing.T) {
	raw := json.RawMessage(`[
		{"type":"message","role":"user","content":[{"type":"input_text","text":"hello"}]},
		{"type":"message","role":"assistant","content":[{"type":"output_text","text":"partial"}]}
	]`)
	input, err := convertToResponsesInput(openai.ResponsesInput{Raw: raw})
	if err != nil {
		t.Fatalf("convert raw input: %v", err)
	}
	if len(input.Items) != 2 || input.Items[0].Role != "user" ||
		input.Items[1].Role != "assistant" || !input.Items[1].Partial {
		t.Fatalf("raw items were not preserved: %+v", input.Items)
	}
}

func TestConvertToResponsesInputRejectsInvalidRawItems(t *testing.T) {
	if _, err := convertToResponsesInput(openai.ResponsesInput{Raw: json.RawMessage(`{"not":"an-array"}`)}); err == nil {
		t.Fatal("invalid raw input was accepted")
	}
}
