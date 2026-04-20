package model

import "testing"

func ptrStr(s string) *string { return &s }

func TestMessageNormalizeDropsEmptyTextParts(t *testing.T) {
	m := Message{
		Role: "user",
		Content: MessageContent{
			MultipleContent: []MessageContentPart{
				{Type: "text", Text: ptrStr("hello")},
				{Type: "text", Text: ptrStr("")},
				{Type: "text", Text: nil},
				{Type: "image_url", ImageURL: &ImageURL{URL: "https://example.com/a.png"}},
				{Type: "", Text: ptrStr("")}, // no explicit type, treated as text
			},
		},
	}
	m.Normalize()
	if len(m.Content.MultipleContent) != 2 {
		t.Fatalf("expected 2 parts after normalize, got %d: %+v", len(m.Content.MultipleContent), m.Content.MultipleContent)
	}
	if m.Content.MultipleContent[0].Type != "text" || *m.Content.MultipleContent[0].Text != "hello" {
		t.Errorf("first part changed: %+v", m.Content.MultipleContent[0])
	}
	if m.Content.MultipleContent[1].Type != "image_url" {
		t.Errorf("image part dropped: %+v", m.Content.MultipleContent[1])
	}
}

func TestMessageNormalizeFillsSpaceOnEmptyAssistant(t *testing.T) {
	m := Message{
		Role: "assistant",
		Content: MessageContent{
			MultipleContent: []MessageContentPart{
				{Type: "text", Text: ptrStr("")},
			},
		},
	}
	m.Normalize()
	if m.Content.Content == nil || *m.Content.Content != " " {
		t.Fatalf("expected single-space placeholder, got %+v", m.Content)
	}
	if len(m.Content.MultipleContent) != 0 {
		t.Errorf("expected MultipleContent cleared, got %+v", m.Content.MultipleContent)
	}
}

func TestMessageNormalizePreservesToolCalls(t *testing.T) {
	m := Message{
		Role: "assistant",
		Content: MessageContent{
			MultipleContent: []MessageContentPart{{Type: "text", Text: ptrStr("")}},
		},
		ToolCalls: []ToolCall{{ID: "call_1", Type: "function", Function: FunctionCall{Name: "lookup"}}},
	}
	m.Normalize()
	if m.Content.Content != nil {
		t.Errorf("should not fill placeholder when tool_calls present: %+v", m.Content)
	}
	if len(m.ToolCalls) != 1 {
		t.Errorf("tool_calls dropped: %+v", m.ToolCalls)
	}
}

func TestMessageNormalizePreservesReasoning(t *testing.T) {
	m := Message{
		Role:    "assistant",
		Content: MessageContent{},
		ReasoningBlocks: []ReasoningBlock{
			{Kind: ReasoningBlockKindThinking, Text: "thinking hard"},
		},
	}
	m.Normalize()
	if m.Content.Content != nil {
		t.Errorf("should not fill placeholder when reasoning_blocks present: %+v", m.Content)
	}
}

func TestMessageNormalizeFillsSpaceWhenFullyEmpty(t *testing.T) {
	m := Message{Role: "user"}
	m.Normalize()
	if m.Content.Content == nil || *m.Content.Content != " " {
		t.Fatalf("expected placeholder on fully-empty msg, got %+v", m.Content)
	}
}

func TestMessageNormalizeIdempotent(t *testing.T) {
	m := Message{
		Role:    "user",
		Content: MessageContent{Content: ptrStr("hi")},
	}
	m.Normalize()
	m.Normalize()
	if *m.Content.Content != "hi" {
		t.Errorf("content mutated: %+v", m.Content.Content)
	}
}

func TestInternalLLMRequestNormalizeMessages(t *testing.T) {
	r := InternalLLMRequest{
		Messages: []Message{
			{Role: "user", Content: MessageContent{MultipleContent: []MessageContentPart{{Type: "text", Text: ptrStr("")}}}},
			{Role: "assistant", Content: MessageContent{Content: ptrStr("ok")}},
		},
	}
	r.NormalizeMessages()
	if r.Messages[0].Content.Content == nil || *r.Messages[0].Content.Content != " " {
		t.Errorf("msg[0] not normalised: %+v", r.Messages[0].Content)
	}
	if *r.Messages[1].Content.Content != "ok" {
		t.Errorf("msg[1] mutated: %+v", r.Messages[1].Content)
	}
}
