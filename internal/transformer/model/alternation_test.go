package model

import "testing"

func TestEnforceAlternationOpenAINoOp(t *testing.T) {
	msgs := []Message{
		{Role: "user", Content: MessageContent{Content: ptrStr("a")}},
		{Role: "user", Content: MessageContent{Content: ptrStr("b")}},
	}
	got := EnforceAlternation(msgs, AlternationProviderOpenAI)
	if len(got) != 2 {
		t.Fatalf("expected no-op for OpenAI, got %d msgs", len(got))
	}
}

func TestEnforceAlternationMergesConsecutiveUser(t *testing.T) {
	msgs := []Message{
		{Role: "user", Content: MessageContent{Content: ptrStr("hi")}},
		{Role: "user", Content: MessageContent{Content: ptrStr("again")}},
		{Role: "assistant", Content: MessageContent{Content: ptrStr("ok")}},
	}
	got := EnforceAlternation(msgs, AlternationProviderAnthropic)
	if len(got) != 2 {
		t.Fatalf("expected 2 msgs after merge, got %d: %+v", len(got), got)
	}
	if *got[0].Content.Content != "hi\n\nagain" {
		t.Errorf("user text merge mismatch: %q", *got[0].Content.Content)
	}
	if *got[1].Content.Content != "ok" {
		t.Errorf("assistant unchanged: %q", *got[1].Content.Content)
	}
}

func TestEnforceAlternationToolBecomesUserForAnthropic(t *testing.T) {
	msgs := []Message{
		{Role: "user", Content: MessageContent{Content: ptrStr("hi")}},
		{Role: "assistant", Content: MessageContent{Content: ptrStr("")}, ToolCalls: []ToolCall{{ID: "c1", Type: "function", Function: FunctionCall{Name: "f"}}}},
		{Role: "tool", ToolCallID: ptrStr("c1"), Content: MessageContent{Content: ptrStr("r1")}},
		{Role: "tool", ToolCallID: ptrStr("c1"), Content: MessageContent{Content: ptrStr("r2")}},
		{Role: "assistant", Content: MessageContent{Content: ptrStr("done")}},
	}
	got := EnforceAlternation(msgs, AlternationProviderAnthropic)
	// user, assistant-tool_calls, tool(merged), assistant → 4 turns
	if len(got) != 4 {
		t.Fatalf("expected 4 msgs, got %d: %+v", len(got), got)
	}
	// The two tool messages should have merged.
	if got[2].Role != "tool" {
		t.Errorf("tool role not preserved internally: %q", got[2].Role)
	}
}

func TestEnforceAlternationInsertsPivotForAssistantFirst(t *testing.T) {
	msgs := []Message{
		{Role: "assistant", Content: MessageContent{Content: ptrStr("greeting")}},
		{Role: "user", Content: MessageContent{Content: ptrStr("hi")}},
	}
	got := EnforceAlternation(msgs, AlternationProviderAnthropic)
	if len(got) != 3 {
		t.Fatalf("expected pivot prepended, got %d: %+v", len(got), got)
	}
	if got[0].Role != "user" || got[0].Content.Content == nil || *got[0].Content.Content != "(continued)" {
		t.Errorf("pivot mismatch: %+v", got[0])
	}
}

func TestEnforceAlternationPreservesSystem(t *testing.T) {
	sys := "system prompt"
	msgs := []Message{
		{Role: "system", Content: MessageContent{Content: &sys}},
		{Role: "user", Content: MessageContent{Content: ptrStr("u1")}},
		{Role: "user", Content: MessageContent{Content: ptrStr("u2")}},
	}
	got := EnforceAlternation(msgs, AlternationProviderAnthropic)
	if len(got) != 2 {
		t.Fatalf("expected system + merged user, got %d: %+v", len(got), got)
	}
	if got[0].Role != "system" {
		t.Errorf("system not preserved: %+v", got[0])
	}
}

func TestEnforceAlternationGeminiMergesModelAsAssistant(t *testing.T) {
	msgs := []Message{
		{Role: "user", Content: MessageContent{Content: ptrStr("q")}},
		{Role: "assistant", Content: MessageContent{Content: ptrStr("partial")}},
		{Role: "model", Content: MessageContent{Content: ptrStr("more")}},
	}
	got := EnforceAlternation(msgs, AlternationProviderGemini)
	if len(got) != 2 {
		t.Fatalf("expected user+merged assistant/model, got %d: %+v", len(got), got)
	}
	if *got[1].Content.Content != "partial\n\nmore" {
		t.Errorf("assistant/model merge mismatch: %q", *got[1].Content.Content)
	}
}

func TestEnforceAlternationEmpty(t *testing.T) {
	got := EnforceAlternation(nil, AlternationProviderAnthropic)
	if got != nil {
		t.Errorf("expected nil for empty input")
	}
}

func TestEnforceAlternationPreservesStructuredParts(t *testing.T) {
	// User with image + subsequent user with text should merge into structured MultipleContent.
	msgs := []Message{
		{Role: "user", Content: MessageContent{MultipleContent: []MessageContentPart{
			{Type: "image_url", ImageURL: &ImageURL{URL: "https://x/a.png"}},
		}}},
		{Role: "user", Content: MessageContent{Content: ptrStr("describe it")}},
	}
	got := EnforceAlternation(msgs, AlternationProviderAnthropic)
	if len(got) != 1 {
		t.Fatalf("expected merged user, got %d: %+v", len(got), got)
	}
	parts := got[0].Content.MultipleContent
	if len(parts) != 2 {
		t.Fatalf("expected 2 parts after merge, got %d: %+v", len(parts), parts)
	}
	if parts[0].Type != "image_url" {
		t.Errorf("image part dropped: %+v", parts[0])
	}
	if parts[1].Type != "text" || parts[1].Text == nil || *parts[1].Text != "describe it" {
		t.Errorf("text part wrong: %+v", parts[1])
	}
}
