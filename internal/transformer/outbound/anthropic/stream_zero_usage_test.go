package anthropic

import (
	"context"
	"testing"

	"github.com/bestruirui/octopus/internal/transformer/model"
)

// lastUsageDelta 返回事件序列中最后一个 UsageDelta 携带的 usage（没有则返回 nil）。
func lastUsageDelta(events []model.StreamEvent) *model.Usage {
	var usage *model.Usage
	for _, ev := range events {
		if ev.Kind == model.StreamEventKindUsageDelta && ev.Usage != nil {
			usage = ev.Usage
		}
	}
	return usage
}

// message_start 返回全零 usage、message_delta 仅带 output 时，output 不应因
// message_start 的零值过滤而丢失。
func TestTransformStreamEventZeroStartOutputOnly(t *testing.T) {
	o := &MessageOutbound{}
	ctx := context.Background()

	if _, err := o.TransformStreamEvent(ctx, []byte(`{"type":"message_start","message":{"id":"msg_1","model":"claude","role":"assistant","usage":{"input_tokens":0,"output_tokens":0}}}`)); err != nil {
		t.Fatalf("message_start: %v", err)
	}
	events, err := o.TransformStreamEvent(ctx, []byte(`{"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":40}}`))
	if err != nil {
		t.Fatalf("message_delta: %v", err)
	}
	usage := lastUsageDelta(events)
	if usage == nil {
		t.Fatalf("expected usage delta, got none: %+v", events)
	}
	if usage.CompletionTokens != 40 {
		t.Fatalf("output lost: got %d want 40", usage.CompletionTokens)
	}
}

// message_start 全零、message_delta 携带真实 input/output 时，真实 input 不应被
// message_start 的零值覆盖（验证条件继承）。
func TestTransformStreamEventZeroStartFullDelta(t *testing.T) {
	o := &MessageOutbound{}
	ctx := context.Background()

	if _, err := o.TransformStreamEvent(ctx, []byte(`{"type":"message_start","message":{"id":"msg_1","model":"claude","role":"assistant","usage":{"input_tokens":0,"output_tokens":0}}}`)); err != nil {
		t.Fatalf("message_start: %v", err)
	}
	events, err := o.TransformStreamEvent(ctx, []byte(`{"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"input_tokens":80,"output_tokens":40}}`))
	if err != nil {
		t.Fatalf("message_delta: %v", err)
	}
	usage := lastUsageDelta(events)
	if usage == nil {
		t.Fatalf("expected usage delta, got none: %+v", events)
	}
	if usage.PromptTokens != 80 {
		t.Fatalf("input overwritten by zero: got %d want 80", usage.PromptTokens)
	}
	if usage.CompletionTokens != 40 {
		t.Fatalf("output lost: got %d want 40", usage.CompletionTokens)
	}
}

// 标准 Anthropic：input 在 message_start、output 在 message_delta，input 应被继承
// （无回归）。
func TestTransformStreamEventStandardInheritsInput(t *testing.T) {
	o := &MessageOutbound{}
	ctx := context.Background()

	if _, err := o.TransformStreamEvent(ctx, []byte(`{"type":"message_start","message":{"id":"msg_1","model":"claude","role":"assistant","usage":{"input_tokens":10,"output_tokens":1}}}`)); err != nil {
		t.Fatalf("message_start: %v", err)
	}
	events, err := o.TransformStreamEvent(ctx, []byte(`{"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":20}}`))
	if err != nil {
		t.Fatalf("message_delta: %v", err)
	}
	usage := lastUsageDelta(events)
	if usage == nil {
		t.Fatalf("expected usage delta, got none: %+v", events)
	}
	if usage.PromptTokens != 10 {
		t.Fatalf("input not inherited: got %d want 10", usage.PromptTokens)
	}
	if usage.CompletionTokens != 20 {
		t.Fatalf("output lost: got %d want 20", usage.CompletionTokens)
	}
}
