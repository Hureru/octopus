package model

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestFlattenUnsupportedBlocksDocument(t *testing.T) {
	r := InternalLLMRequest{
		Messages: []Message{
			{
				Role: "user",
				Content: MessageContent{
					MultipleContent: []MessageContentPart{
						{Type: "text", Text: ptrStr("please read:")},
						{Type: "document", Document: &DocumentSource{
							Type:      "url",
							URL:       "https://example.com/report.pdf",
							Title:     "Q1 Report",
							MediaType: "application/pdf",
						}},
					},
				},
			},
		},
	}
	r.FlattenUnsupportedBlocks(AlternationProviderOpenAI)
	parts := r.Messages[0].Content.MultipleContent
	if len(parts) != 2 {
		t.Fatalf("expected 2 parts after flatten, got %d: %+v", len(parts), parts)
	}
	if parts[0].Type != "text" {
		t.Errorf("first part should be text, got %+v", parts[0])
	}
	if parts[1].Type != "text" {
		t.Errorf("doc should have been flattened to text, got %+v", parts[1])
	}
	if parts[1].Text == nil || !strings.Contains(*parts[1].Text, "Q1 Report") {
		t.Errorf("doc hint missing title: %v", parts[1].Text)
	}
	if !strings.Contains(*parts[1].Text, "example.com/report.pdf") {
		t.Errorf("doc hint missing url: %v", parts[1].Text)
	}
}

func TestFlattenUnsupportedBlocksServerTool(t *testing.T) {
	r := InternalLLMRequest{
		Messages: []Message{
			{
				Role: "assistant",
				Content: MessageContent{
					MultipleContent: []MessageContentPart{
						{Type: "text", Text: ptrStr("based on search:")},
						{Type: "server_tool_use", ServerToolUse: &ServerToolUseBlock{
							ID: "srv_1", Name: "web_search_20250305",
						}},
						{Type: "server_tool_result", ServerToolResult: &ServerToolResultBlock{
							ToolUseID: "srv_1",
						}},
					},
				},
			},
		},
	}
	r.FlattenUnsupportedBlocks(AlternationProviderOpenAI)
	parts := r.Messages[0].Content.MultipleContent
	if len(parts) != 1 {
		t.Fatalf("expected server_tool blocks dropped, got %d: %+v", len(parts), parts)
	}
	if parts[0].Type != "text" {
		t.Errorf("survivor should be text: %+v", parts[0])
	}
}

func TestFlattenUnsupportedBlocksAnthropicNoOp(t *testing.T) {
	// Anthropic natively supports document / server_tool blocks; Flatten
	// should leave them untouched.
	r := InternalLLMRequest{
		Messages: []Message{
			{
				Role: "user",
				Content: MessageContent{
					MultipleContent: []MessageContentPart{
						{Type: "document", Document: &DocumentSource{Type: "url", URL: "https://x"}},
					},
				},
			},
		},
	}
	r.FlattenUnsupportedBlocks(AlternationProviderAnthropic)
	if r.Messages[0].Content.MultipleContent[0].Type != "document" {
		t.Errorf("document should not have been flattened for Anthropic")
	}
}

func TestDocumentSourceJSONNotLeakedOnChat(t *testing.T) {
	// Document/ServerToolUse/ServerToolResult carry json:"-" so marshalling
	// a MessageContentPart for OpenAI chat completions doesn't leak the
	// Anthropic-specific blob shape.
	text := "dummy"
	part := MessageContentPart{
		Type:     "text",
		Text:     &text,
		Document: &DocumentSource{Type: "base64", Data: "aGVsbG8="},
		ServerToolUse: &ServerToolUseBlock{
			ID: "srv_1", Name: "web_search_20250305",
		},
	}
	b, err := json.Marshal(part)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	s := string(b)
	if strings.Contains(s, "document") {
		t.Errorf("document leaked into JSON: %s", s)
	}
	if strings.Contains(s, "server_tool_use") {
		t.Errorf("server_tool_use leaked into JSON: %s", s)
	}
}

func TestMessageNormalizeDocumentPartsSurvive(t *testing.T) {
	// Normalize must keep document / server_tool_use as non-empty parts
	// even though their Text field is nil.
	m := Message{
		Role: "user",
		Content: MessageContent{
			MultipleContent: []MessageContentPart{
				{Type: "document", Document: &DocumentSource{Type: "url", URL: "https://x/a.pdf"}},
				{Type: "text", Text: ptrStr("")},
			},
		},
	}
	m.Normalize()
	if len(m.Content.MultipleContent) != 1 {
		t.Fatalf("expected 1 part after normalize, got %d: %+v", len(m.Content.MultipleContent), m.Content.MultipleContent)
	}
	if m.Content.MultipleContent[0].Type != "document" {
		t.Errorf("document part dropped: %+v", m.Content.MultipleContent[0])
	}
}
