package model

import (
	"encoding/json"
	"testing"
)

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
