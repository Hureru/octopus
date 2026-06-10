package openai

import (
	"context"
	"strings"
	"testing"

	"github.com/bestruirui/octopus/internal/transformer/model"
)

// Issue #65 Bug 1: model.InternalResponseFromStreamEvents returns nil when the
// aggregate carries no choices, usage, or error, and TransformStreamEvents fed
// that nil straight into TransformStream, which dereferenced stream.Object.
// The relay only guards len(events)==0, so non-empty nil-producing sequences
// reached the panic in production (e.g. usage-less UsageDelta events).
func TestTransformStreamEventsNilAggregateDoesNotPanic(t *testing.T) {
	tests := []struct {
		name   string
		events []model.StreamEvent
	}{
		{name: "empty slice", events: []model.StreamEvent{}},
		{name: "usage_delta with nil Usage", events: []model.StreamEvent{{Kind: model.StreamEventKindUsageDelta, Usage: nil}}},
		{name: "error event with nil Error", events: []model.StreamEvent{{Kind: model.StreamEventKindError, Error: nil}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			inbound := &ChatInbound{}
			data, err := inbound.TransformStreamEvents(context.Background(), tt.events)
			if err != nil {
				t.Fatalf("expected nil error for nil aggregate, got %v", err)
			}
			if data != nil {
				t.Fatalf("expected nil data for nil aggregate, got %q", data)
			}
		})
	}
}

// A tool call stream as emitted by the fixed Responses outbound (choice index
// 0, dense tool index) must produce a forwardable chat chunk instead of the
// pre-fix nil panic / silent drop.
func TestTransformStreamEventsToolCallStreamProducesChunk(t *testing.T) {
	toolCall := model.ToolCall{
		Index: 0,
		ID:    "call_abc123",
		Type:  "function",
		Function: model.FunctionCall{
			Name: "get_weather",
		},
	}
	events := []model.StreamEvent{
		{Kind: model.StreamEventKindToolCallStart, ID: "resp_123", Model: "gpt-test", Index: 0, ToolCall: &toolCall},
		{Kind: model.StreamEventKindToolCallDelta, ID: "resp_123", Model: "gpt-test", Index: 0, ToolCall: &toolCall, Delta: &model.StreamDelta{Arguments: "{\"city\":"}},
	}

	inbound := &ChatInbound{}
	data, err := inbound.TransformStreamEvents(context.Background(), events)
	if err != nil {
		t.Fatalf("TransformStreamEvents: %v", err)
	}
	chunk := string(data)
	if !strings.HasPrefix(chunk, "data: ") {
		t.Fatalf("expected SSE data line, got %q", chunk)
	}
	if !strings.Contains(chunk, `"index":0`) || !strings.Contains(chunk, "get_weather") {
		t.Fatalf("expected chunk with choice 0 carrying the tool call, got %q", chunk)
	}
}
