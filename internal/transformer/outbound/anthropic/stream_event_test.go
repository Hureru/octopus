package anthropic

import (
	"context"
	"testing"

	"github.com/bestruirui/octopus/internal/transformer/model"
)

func TestTransformStreamEventAnthropicNativeMapping(t *testing.T) {
	outbound := &MessageOutbound{}
	ctx := context.Background()

	// Test message_start
	startChunk := []byte(`{"type":"message_start","message":{"id":"msg_1","model":"claude-3-5-sonnet","role":"assistant","usage":{"input_tokens":10,"output_tokens":1}}}`)
	events, err := outbound.TransformStreamEvent(ctx, startChunk)
	if err != nil {
		t.Fatalf("message_start: %v", err)
	}
	if len(events) == 0 || events[0].Kind != model.StreamEventKindMessageStart {
		t.Fatalf("expected MessageStart, got %+v", events)
	}
	if events[0].ID != "msg_1" || events[0].Model != "claude-3-5-sonnet" {
		t.Fatalf("metadata lost: %+v", events[0])
	}

	// Test thinking_delta
	thinkingChunk := []byte(`{"type":"content_block_delta","index":0,"delta":{"type":"thinking_delta","thinking":"plan"}}`)
	events, err = outbound.TransformStreamEvent(ctx, thinkingChunk)
	if err != nil {
		t.Fatalf("thinking_delta: %v", err)
	}
	if len(events) == 0 || events[0].Kind != model.StreamEventKindThinkingDelta {
		t.Fatalf("expected ThinkingDelta, got %+v", events)
	}
	if events[0].Delta == nil || events[0].Delta.Thinking != "plan" {
		t.Fatalf("thinking text lost: %+v", events[0].Delta)
	}

	// Test signature_delta
	sigChunk := []byte(`{"type":"content_block_delta","index":0,"delta":{"type":"signature_delta","signature":"sig-abc"}}`)
	events, err = outbound.TransformStreamEvent(ctx, sigChunk)
	if err != nil {
		t.Fatalf("signature_delta: %v", err)
	}
	if len(events) == 0 || events[0].Kind != model.StreamEventKindSignatureDelta {
		t.Fatalf("expected SignatureDelta, got %+v", events)
	}
	if events[0].Delta == nil || events[0].Delta.Signature != "sig-abc" {
		t.Fatalf("signature lost: %+v", events[0].Delta)
	}

	// Test text_delta
	textChunk := []byte(`{"type":"content_block_delta","index":1,"delta":{"type":"text_delta","text":"hello"}}`)
	events, err = outbound.TransformStreamEvent(ctx, textChunk)
	if err != nil {
		t.Fatalf("text_delta: %v", err)
	}
	if len(events) == 0 || events[0].Kind != model.StreamEventKindTextDelta {
		t.Fatalf("expected TextDelta, got %+v", events)
	}
	if events[0].Delta == nil || events[0].Delta.Text != "hello" {
		t.Fatalf("text lost: %+v", events[0].Delta)
	}

	// Test tool_use start
	toolStartChunk := []byte(`{"type":"content_block_start","index":2,"content_block":{"type":"tool_use","id":"call_1","name":"lookup"}}`)
	events, err = outbound.TransformStreamEvent(ctx, toolStartChunk)
	if err != nil {
		t.Fatalf("tool_use start: %v", err)
	}
	if len(events) == 0 || events[0].Kind != model.StreamEventKindToolCallStart {
		t.Fatalf("expected ToolCallStart, got %+v", events)
	}
	if events[0].ToolCall == nil || events[0].ToolCall.ID != "call_1" || events[0].ToolCall.Function.Name != "lookup" {
		t.Fatalf("tool call metadata lost: %+v", events[0].ToolCall)
	}

	// Test input_json_delta
	toolDeltaChunk := []byte(`{"type":"content_block_delta","index":2,"delta":{"type":"input_json_delta","partial_json":"{\"q\":\"x\"}"}}`)
	events, err = outbound.TransformStreamEvent(ctx, toolDeltaChunk)
	if err != nil {
		t.Fatalf("input_json_delta: %v", err)
	}
	if len(events) == 0 || events[0].Kind != model.StreamEventKindToolCallDelta {
		t.Fatalf("expected ToolCallDelta, got %+v", events)
	}
	if events[0].Delta == nil || events[0].Delta.Arguments != `{"q":"x"}` {
		t.Fatalf("tool arguments lost: %+v", events[0].Delta)
	}

	// Test message_delta with usage
	msgDeltaChunk := []byte(`{"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":20}}`)
	events, err = outbound.TransformStreamEvent(ctx, msgDeltaChunk)
	if err != nil {
		t.Fatalf("message_delta: %v", err)
	}
	foundStop := false
	foundUsage := false
	for _, ev := range events {
		if ev.Kind == model.StreamEventKindMessageStop {
			foundStop = true
			if ev.StopReason != model.FinishReasonStop {
				t.Fatalf("stop reason mismatch: %v", ev.StopReason)
			}
		}
		if ev.Kind == model.StreamEventKindUsageDelta {
			foundUsage = true
			if ev.Usage == nil || ev.Usage.CompletionTokens != 20 {
				t.Fatalf("usage lost: %+v", ev.Usage)
			}
		}
	}
	if !foundStop || !foundUsage {
		t.Fatalf("expected stop+usage, got %+v", events)
	}

	// Test [DONE]
	doneChunk := []byte(`[DONE]`)
	events, err = outbound.TransformStreamEvent(ctx, doneChunk)
	if err != nil {
		t.Fatalf("[DONE]: %v", err)
	}
	if len(events) != 1 || events[0].Kind != model.StreamEventKindDone {
		t.Fatalf("expected Done, got %+v", events)
	}

	// Test error
	errorChunk := []byte(`{"type":"error","error":{"type":"overloaded_error","message":"Overloaded"}}`)
	events, err = outbound.TransformStreamEvent(ctx, errorChunk)
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	if len(events) != 1 || events[0].Kind != model.StreamEventKindError {
		t.Fatalf("expected Error, got %+v", events)
	}
	if events[0].Error == nil || events[0].Error.Detail.Message != "Overloaded" {
		t.Fatalf("error detail lost: %+v", events[0].Error)
	}
}
