package gemini

import (
	"testing"

	"github.com/bestruirui/octopus/internal/transformer/model"
)

func TestCleanGeminiSchemaRemovesPropertyNamesRecursively(t *testing.T) {
	schema := map[string]any{
		"type": "object",
		"propertyNames": map[string]any{
			"type": "string",
		},
		"properties": map[string]any{
			"payload": map[string]any{
				"type": "object",
				"propertyNames": map[string]any{
					"pattern": "^[a-z]+$",
				},
			},
		},
	}

	cleanGeminiSchema(schema)

	if _, ok := schema["propertyNames"]; ok {
		t.Fatalf("expected top-level propertyNames to be removed")
	}
	props := schema["properties"].(map[string]any)
	payload := props["payload"].(map[string]any)
	if _, ok := payload["propertyNames"]; ok {
		t.Fatalf("expected nested propertyNames to be removed")
	}
}

func TestConvertGeminiRequestBindsToolCallThoughtSignature(t *testing.T) {
	req := &model.InternalLLMRequest{
		Model: "gemini-3.1-pro",
		Messages: []model.Message{
			{
				Role: "assistant",
				Content: model.MessageContent{
					Content: stringPtr("I will call a tool."),
				},
				ReasoningBlocks: []model.ReasoningBlock{
					{Kind: model.ReasoningBlockKindThinking, Text: "thinking", Signature: "sig-thought", Provider: "gemini"},
					{Kind: model.ReasoningBlockKindSignature, Signature: "sig-call", Provider: "gemini"},
				},
				ToolCalls: []model.ToolCall{
					{
						ID:   "call-1",
						Type: "function",
						Function: model.FunctionCall{
							Name:      "Bash",
							Arguments: `{"cmd":"pwd"}`,
						},
					},
				},
			},
		},
	}

	out := convertLLMToGeminiRequest(req)
	parts := out.Contents[0].Parts
	if len(parts) != 3 {
		t.Fatalf("expected 3 parts, got %d: %+v", len(parts), parts)
	}
	if !parts[0].Thought || parts[0].ThoughtSignature != "sig-thought" {
		t.Fatalf("expected first part to be replayed thought with signature, got %+v", parts[0])
	}
	if parts[1].Text != "I will call a tool." || parts[1].ThoughtSignature != "" {
		t.Fatalf("expected visible text part without signature, got %+v", parts[1])
	}
	if parts[2].FunctionCall == nil || parts[2].ThoughtSignature != "sig-call" {
		t.Fatalf("expected functionCall part to keep its own signature, got %+v", parts[2])
	}
}

func TestConvertGeminiRequestDowngradesUnsignedHistoricalToolUse(t *testing.T) {
	req := &model.InternalLLMRequest{
		Model: "gemini-3.1-pro",
		Messages: []model.Message{
			{
				Role: "assistant",
				ToolCalls: []model.ToolCall{
					{
						ID:   "call-1",
						Type: "function",
						Function: model.FunctionCall{
							Name:      "Bash",
							Arguments: `{"cmd":"ls"}`,
						},
					},
				},
			},
			{
				Role:       "tool",
				ToolCallID: stringPtr("call-1"),
				Content: model.MessageContent{
					Content: stringPtr("tool output"),
				},
			},
		},
	}

	out := convertLLMToGeminiRequest(req)
	if got := out.Contents[0].Parts[0].FunctionCall; got != nil {
		t.Fatalf("expected unsigned tool call to downgrade to text, got %+v", out.Contents[0].Parts[0])
	}
	if out.Contents[1].Parts[0].FunctionResponse != nil {
		t.Fatalf("expected matching tool result to downgrade to text, got %+v", out.Contents[1].Parts[0])
	}
}

func TestDecodeGeminiToolResponseAcceptsScalarJSON(t *testing.T) {
	decoded, ok := decodeGeminiToolResponse(`true`)
	if !ok {
		t.Fatalf("expected scalar JSON to decode")
	}
	if got, ok := decoded["result"].(bool); !ok || !got {
		t.Fatalf("expected scalar JSON wrapped under result, got %+v", decoded)
	}
}

func stringPtr(v string) *string {
	return &v
}
