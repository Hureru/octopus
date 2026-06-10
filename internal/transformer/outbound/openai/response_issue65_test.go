package openai

import (
	"context"
	"testing"

	"github.com/bestruirui/octopus/internal/transformer/model"
)

// Issue #65: reasoning models place a reasoning item at output_index 0, so
// function_call items land at output_index >= 1. The outbound used the raw
// output_index both as the StreamEvent choice index (collapsing the chat
// aggregate to nil and panicking ChatInbound) and as the client-visible
// tool_calls[].index (sparse, non-zero-based). Function call events must sit
// on choice 0 with densely renumbered tool call indices.
func TestTransformStreamEventFunctionCallAtNonZeroOutputIndex(t *testing.T) {
	o := &ResponseOutbound{}
	ctx := context.Background()

	added := `{"type":"response.output_item.added","output_index":1,"item":{"type":"function_call","call_id":"call_1","name":"get_weather"}}`
	events, err := o.TransformStreamEvent(ctx, []byte(added))
	if err != nil {
		t.Fatalf("output_item.added: %v", err)
	}
	if len(events) != 1 || events[0].Kind != model.StreamEventKindToolCallStart {
		t.Fatalf("expected single tool_call_start event, got %+v", events)
	}
	if events[0].Index != 0 {
		t.Fatalf("function call event must carry choice index 0, got %d", events[0].Index)
	}
	if events[0].ToolCall == nil || events[0].ToolCall.Index != 0 {
		t.Fatalf("first function call must get dense tool index 0, got %+v", events[0].ToolCall)
	}

	delta := `{"type":"response.function_call_arguments.delta","output_index":1,"call_id":"call_1","name":"get_weather","delta":"{\"city\":"}`
	events, err = o.TransformStreamEvent(ctx, []byte(delta))
	if err != nil {
		t.Fatalf("function_call_arguments.delta: %v", err)
	}
	if len(events) != 2 {
		t.Fatalf("expected tool_call_start + tool_call_delta, got %+v", events)
	}
	for _, ev := range events {
		if ev.Index != 0 {
			t.Fatalf("function call event must carry choice index 0, got %d", ev.Index)
		}
		if ev.ToolCall == nil || ev.ToolCall.Index != 0 {
			t.Fatalf("same output_index must keep dense tool index 0, got %+v", ev.ToolCall)
		}
	}

	// A second parallel call at output_index 2 gets the next dense index.
	added2 := `{"type":"response.output_item.added","output_index":2,"item":{"type":"function_call","call_id":"call_2","name":"get_time"}}`
	events, err = o.TransformStreamEvent(ctx, []byte(added2))
	if err != nil {
		t.Fatalf("second output_item.added: %v", err)
	}
	if len(events) != 1 || events[0].ToolCall == nil || events[0].ToolCall.Index != 1 {
		t.Fatalf("second function call must get dense tool index 1, got %+v", events)
	}
	if events[0].Index != 0 {
		t.Fatalf("second function call event must still carry choice index 0, got %d", events[0].Index)
	}

	// The aggregate of the full sequence must be a usable chat chunk: both
	// calls on choice 0, distinguishable by their dense tool indices.
	all := []model.StreamEvent{}
	o2 := &ResponseOutbound{}
	for _, raw := range []string{added, delta, added2} {
		evs, err := o2.TransformStreamEvent(ctx, []byte(raw))
		if err != nil {
			t.Fatalf("replay: %v", err)
		}
		all = append(all, evs...)
	}
	resp := model.InternalResponseFromStreamEvents(all)
	if resp == nil {
		t.Fatalf("aggregate must not be nil (this was the issue #65 panic trigger)")
	}
	if len(resp.Choices) != 1 || resp.Choices[0].Index != 0 {
		t.Fatalf("expected single choice 0, got %+v", resp.Choices)
	}
	toolCalls := resp.Choices[0].Delta.ToolCalls
	if len(toolCalls) != 2 || toolCalls[0].Index != 0 || toolCalls[1].Index != 1 {
		t.Fatalf("expected two dense tool calls (0,1), got %+v", toolCalls)
	}
}
