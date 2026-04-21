package anthropic

import (
	"context"
	"testing"
)

func TestMessageContentUnmarshalAcceptsSingleBlockObject(t *testing.T) {
	var content MessageContent
	if err := content.UnmarshalJSON([]byte(`{"type":"text","text":"hello"}`)); err != nil {
		t.Fatalf("expected single block object to be accepted, got %v", err)
	}
	if content.Content != nil {
		t.Fatalf("expected string content to remain nil, got %#v", content.Content)
	}
	if len(content.MultipleContent) != 1 || content.MultipleContent[0].Type != "text" {
		t.Fatalf("expected one text block, got %#v", content.MultipleContent)
	}
	if content.MultipleContent[0].Text == nil || *content.MultipleContent[0].Text != "hello" {
		t.Fatalf("expected text block payload to be preserved, got %#v", content.MultipleContent[0])
	}
}

func TestTransformRequestAcceptsToolResultContentAsSingleBlockObject(t *testing.T) {
	inbound := &MessagesInbound{}
	body := []byte(`{
		"model":"claude-3-5-sonnet",
		"max_tokens":16,
		"messages":[
			{
				"role":"user",
				"content":[
					{
						"type":"tool_result",
						"tool_use_id":"toolu_123",
						"content":{"type":"text","text":"tool ok"}
					}
				]
			}
		]
	}`)

	req, err := inbound.TransformRequest(context.Background(), body)
	if err != nil {
		t.Fatalf("TransformRequest() error = %v", err)
	}
	if len(req.Messages) != 1 {
		t.Fatalf("expected one internal message, got %#v", req.Messages)
	}
	msg := req.Messages[0]
	if msg.Role != "tool" {
		t.Fatalf("expected tool role after tool_result conversion, got %#v", msg.Role)
	}
	if msg.ToolCallID == nil || *msg.ToolCallID != "toolu_123" {
		t.Fatalf("expected tool_call_id to be preserved, got %#v", msg.ToolCallID)
	}
	if len(msg.Content.MultipleContent) != 1 {
		t.Fatalf("expected tool result to become one content part, got %#v", msg.Content)
	}
	part := msg.Content.MultipleContent[0]
	if part.Type != "text" || part.Text == nil || *part.Text != "tool ok" {
		t.Fatalf("expected tool result text part to be preserved, got %#v", part)
	}
}
