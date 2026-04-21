package anthropic

import (
	"testing"

	anthropicModel "github.com/bestruirui/octopus/internal/transformer/inbound/anthropic"
)

func TestConvertToLLMResponsePropagatesStopSequence(t *testing.T) {
	reason := "stop_sequence"
	sequence := "###END###"
	msg := &anthropicModel.Message{
		ID:           "msg_01",
		Type:         "message",
		Role:         "assistant",
		Model:        "claude-3-5-sonnet",
		Content:      []anthropicModel.MessageContentBlock{{Type: "text", Text: stringPtr("hi")}},
		StopReason:   &reason,
		StopSequence: &sequence,
	}

	resp := convertToLLMResponse(msg)
	if len(resp.Choices) != 1 {
		t.Fatalf("expected 1 choice, got %d", len(resp.Choices))
	}
	got := resp.Choices[0].StopSequence
	if got == nil || *got != sequence {
		t.Fatalf("expected stop_sequence %q propagated, got %v", sequence, got)
	}
	if resp.Choices[0].FinishReason == nil || *resp.Choices[0].FinishReason != "stop_sequence" {
		t.Fatalf("expected finish_reason stop_sequence, got %v", resp.Choices[0].FinishReason)
	}
}
