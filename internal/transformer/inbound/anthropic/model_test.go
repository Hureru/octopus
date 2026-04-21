package anthropic

import (
	"context"
	"encoding/json"
	"strings"
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

// A-H5: Server tools like `web_search_20250305` must preserve their raw body
// so outbound can replay spec-specific fields. Function tools keep working
// unchanged.
func TestToolUnmarshalPreservesServerToolRawBody(t *testing.T) {
	raw := `{"type":"web_search_20250305","name":"web_search","max_uses":3,"allowed_domains":["a.com"]}`
	var tool Tool
	if err := json.Unmarshal([]byte(raw), &tool); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if tool.Type != "web_search_20250305" {
		t.Fatalf("expected type preserved, got %q", tool.Type)
	}
	if !tool.IsServerTool() {
		t.Fatalf("expected IsServerTool=true")
	}
	if len(tool.RawBody) == 0 {
		t.Fatalf("expected raw body preserved")
	}
	out, err := json.Marshal(tool)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if !strings.Contains(string(out), "max_uses") {
		t.Fatalf("expected spec-specific fields in marshaled wire body, got %s", string(out))
	}
	if !strings.Contains(string(out), "allowed_domains") {
		t.Fatalf("expected allowed_domains to survive, got %s", string(out))
	}
}

// A-H5 (negative): function tools still use the default marshal path and
// do not grow new fields (type, name, description, input_schema).
func TestToolUnmarshalFunctionToolUnchanged(t *testing.T) {
	raw := `{"name":"lookup","description":"look it up","input_schema":{"type":"object"}}`
	var tool Tool
	if err := json.Unmarshal([]byte(raw), &tool); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if tool.IsServerTool() {
		t.Fatalf("function tool misclassified as server tool")
	}
	out, err := json.Marshal(tool)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var generic map[string]json.RawMessage
	if err := json.Unmarshal(out, &generic); err != nil {
		t.Fatalf("unmarshal back: %v", err)
	}
	if _, ok := generic["type"]; ok {
		t.Fatalf("function tool should omit top-level type when unset, got %s", string(out))
	}
	if _, ok := generic["input_schema"]; !ok {
		t.Fatalf("expected input_schema to be preserved, got %s", string(out))
	}
}
