package model

import "testing"

// Issue #65: the choice rebuild loops iterated idx 0..len(map)-1 over maps
// keyed by event/choice index, silently dropping every choice stored at a
// non-contiguous index (e.g. tool calls at index >= 1) and — in
// InternalResponseFromStreamEvents — collapsing the whole aggregate to nil,
// which ChatInbound then dereferenced.

func TestInternalResponseFromStreamEventsKeepsNonContiguousChoiceIndices(t *testing.T) {
	events := []StreamEvent{
		{Kind: StreamEventKindTextDelta, ID: "resp_1", Model: "gpt-test", Index: 1, Delta: &StreamDelta{Text: "hello"}},
	}
	resp := InternalResponseFromStreamEvents(events)
	if resp == nil {
		t.Fatalf("expected non-nil response for events at Index=1, got nil")
	}
	if len(resp.Choices) != 1 || resp.Choices[0].Index != 1 {
		t.Fatalf("expected single choice preserving Index=1, got %+v", resp.Choices)
	}
	if resp.Choices[0].Delta == nil || resp.Choices[0].Delta.Content.Content == nil || *resp.Choices[0].Delta.Content.Content != "hello" {
		t.Fatalf("expected text delta to survive, got %+v", resp.Choices[0].Delta)
	}
}

func TestInternalResponseFromStreamEventsStillNilForNoContent(t *testing.T) {
	tests := []struct {
		name   string
		events []StreamEvent
	}{
		{name: "empty slice", events: []StreamEvent{}},
		{name: "usage_delta with nil Usage", events: []StreamEvent{{Kind: StreamEventKindUsageDelta, Usage: nil}}},
		{name: "error event with nil Error", events: []StreamEvent{{Kind: StreamEventKindError, Error: nil}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if resp := InternalResponseFromStreamEvents(tt.events); resp != nil {
				t.Fatalf("expected nil response, got %+v", resp)
			}
		})
	}
}

// A chat chunk carrying a parallel tool call (tool_calls[].index=1 on choice 0)
// must produce events keyed by the choice index, not the tool call index —
// otherwise the round-trip rebuild re-homes the call into a phantom choice 1
// (or, before issue #65 was fixed, returns nil and panics ChatInbound).
func TestStreamEventsFromInternalResponseUsesChoiceIndexForToolCalls(t *testing.T) {
	chunk := &InternalLLMResponse{
		ID:     "chatcmpl-1",
		Object: "chat.completion.chunk",
		Model:  "gpt-test",
		Choices: []Choice{
			{
				Index: 0,
				Delta: &Message{
					ToolCalls: []ToolCall{
						{Index: 1, ID: "call_2", Type: "function", Function: FunctionCall{Name: "get_weather", Arguments: `{"city":"sf"}`}},
					},
				},
			},
		},
	}

	events := StreamEventsFromInternalResponse(chunk)
	if len(events) == 0 {
		t.Fatalf("expected events, got none")
	}
	for _, ev := range events {
		if ev.Kind == StreamEventKindToolCallDelta && ev.Index != 0 {
			t.Fatalf("tool call event must carry choice index 0, got Index=%d", ev.Index)
		}
	}

	rebuilt := InternalResponseFromStreamEvents(events)
	if rebuilt == nil {
		t.Fatalf("round-trip returned nil")
	}
	if len(rebuilt.Choices) != 1 || rebuilt.Choices[0].Index != 0 {
		t.Fatalf("expected single choice 0 after round-trip, got %+v", rebuilt.Choices)
	}
	toolCalls := rebuilt.Choices[0].Delta.ToolCalls
	if len(toolCalls) != 1 || toolCalls[0].Index != 1 || toolCalls[0].ID != "call_2" {
		t.Fatalf("expected tool call index=1 preserved on choice 0, got %+v", toolCalls)
	}
}

func TestStreamAggregatorKeepsNonContiguousChoiceIndices(t *testing.T) {
	text := "hi"
	var agg StreamAggregator
	agg.Add(&InternalLLMResponse{
		ID:     "chatcmpl-1",
		Object: "chat.completion.chunk",
		Model:  "gpt-test",
		Choices: []Choice{
			{Index: 1, Delta: &Message{Content: MessageContent{Content: &text}}},
		},
	})
	resp := agg.BuildAndReset()
	if resp == nil {
		t.Fatalf("expected aggregated response, got nil")
	}
	if len(resp.Choices) != 1 || resp.Choices[0].Index != 1 {
		t.Fatalf("expected choice at index 1 to survive aggregation, got %+v", resp.Choices)
	}
}
