package gemini

import (
	"encoding/json"
	"strings"
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

// TestConvertGeminiRequestFunctionResponseName verifies that a signed
// assistant→tool turn reaches Gemini with functionResponse.name equal to the
// originating functionCall.name, not the tool-call ID. Prior implementation
// filled Name with msg.ToolCallID, producing
// `INVALID_ARGUMENT: Function response name does not match any function call
// name` on any non-single-turn flow. (G-C2)
func TestConvertGeminiRequestFunctionResponseNameFromAssistantLookup(t *testing.T) {
	req := &model.InternalLLMRequest{
		Model: "gemini-3.1-pro",
		Messages: []model.Message{
			{
				Role: "assistant",
				ReasoningBlocks: []model.ReasoningBlock{
					{Kind: model.ReasoningBlockKindSignature, Signature: "sig-call", Provider: "gemini"},
				},
				ToolCalls: []model.ToolCall{
					{
						ID:   "call_Bash_0",
						Type: "function",
						Function: model.FunctionCall{
							Name:      "Bash",
							Arguments: `{"cmd":"pwd"}`,
						},
					},
				},
			},
			{
				Role:       "tool",
				ToolCallID: stringPtr("call_Bash_0"),
				Content: model.MessageContent{
					Content: stringPtr(`{"stdout":"/tmp"}`),
				},
			},
		},
	}
	out := convertLLMToGeminiRequest(req)
	if len(out.Contents) < 2 {
		t.Fatalf("expected assistant + tool contents, got %d", len(out.Contents))
	}
	toolContent := out.Contents[1]
	fr := toolContent.Parts[0].FunctionResponse
	if fr == nil {
		t.Fatalf("expected functionResponse part, got %+v", toolContent.Parts[0])
	}
	if fr.Name != "Bash" {
		t.Fatalf("expected functionResponse.name=%q, got %q", "Bash", fr.Name)
	}
}

// TestConvertGeminiRequestFunctionResponseNamePrefersToolCallName covers the
// case where the inbound layer already resolved the function name and placed
// it on Message.ToolCallName; this path should win over the ID lookup.
func TestConvertGeminiRequestFunctionResponseNamePrefersToolCallName(t *testing.T) {
	nameOnly := "preferred_name"
	req := &model.InternalLLMRequest{
		Model: "gemini-2.5-flash",
		Messages: []model.Message{
			{
				Role:         "tool",
				ToolCallID:   stringPtr("call_99"),
				ToolCallName: &nameOnly,
				Content: model.MessageContent{
					Content: stringPtr(`{"ok":true}`),
				},
			},
		},
	}
	out := convertLLMToGeminiRequest(req)
	if len(out.Contents) != 1 {
		t.Fatalf("expected 1 content, got %d", len(out.Contents))
	}
	fr := out.Contents[0].Parts[0].FunctionResponse
	if fr == nil {
		t.Fatalf("expected functionResponse part, got %+v", out.Contents[0].Parts[0])
	}
	if fr.Name != nameOnly {
		t.Fatalf("expected functionResponse.name=%q, got %q", nameOnly, fr.Name)
	}
}

// TestConvertGeminiResponseCodeExecutionParts verifies G-H9: when Gemini
// returns ExecutableCode and CodeExecutionResult parts (emitted by the
// sandboxed code_execution tool), the outbound transformer folds them
// into MessageContentPart entries with ServerToolUse / ServerToolResult
// envelopes so the existing cross-provider passthrough picks them up.
// Previously these parts were silently dropped during unmarshaling.
func TestConvertGeminiResponseCodeExecutionParts(t *testing.T) {
	reason := "STOP"
	resp := &model.GeminiGenerateContentResponse{
		Candidates: []*model.GeminiCandidate{
			{
				Index:        0,
				FinishReason: &reason,
				Content: &model.GeminiContent{
					Role: "model",
					Parts: []*model.GeminiPart{
						{Text: "Let me compute that."},
						{ExecutableCode: &model.GeminiExecutableCode{
							Language: "PYTHON",
							Code:     "print(1+1)",
						}},
						{CodeExecutionResult: &model.GeminiCodeExecutionResult{
							Outcome: "OUTCOME_OK",
							Output:  "2\n",
						}},
					},
				},
			},
		},
	}
	internal := convertGeminiToLLMResponse(resp, false, nil)
	if len(internal.Choices) != 1 {
		t.Fatalf("expected 1 choice, got %d", len(internal.Choices))
	}
	msg := internal.Choices[0].Message
	if msg == nil {
		t.Fatalf("expected message, got nil")
	}
	parts := msg.Content.MultipleContent
	if len(parts) < 2 {
		t.Fatalf("expected at least 2 structured parts, got %d: %+v", len(parts), parts)
	}

	var sawUse, sawResult bool
	for _, p := range parts {
		if p.Type == "server_tool_use" && p.ServerToolUse != nil {
			sawUse = true
			if p.ServerToolUse.Name != "code_execution" {
				t.Errorf("expected server_tool_use.Name=code_execution, got %q", p.ServerToolUse.Name)
			}
			if !strings.Contains(string(p.ServerToolUse.Input), "PYTHON") {
				t.Errorf("expected server_tool_use.Input to carry language, got %s", string(p.ServerToolUse.Input))
			}
		}
		if p.Type == "server_tool_result" && p.ServerToolResult != nil {
			sawResult = true
			if p.ServerToolResult.BlockType != "code_execution_tool_result" {
				t.Errorf("expected BlockType=code_execution_tool_result, got %q", p.ServerToolResult.BlockType)
			}
			if p.ServerToolResult.IsError == nil || *p.ServerToolResult.IsError {
				t.Errorf("expected IsError=false for OUTCOME_OK, got %+v", p.ServerToolResult.IsError)
			}
			if !strings.Contains(string(p.ServerToolResult.Content), "2") {
				t.Errorf("expected result.Content to carry output, got %s", string(p.ServerToolResult.Content))
			}
		}
	}
	if !sawUse {
		t.Errorf("expected server_tool_use part, got %+v", parts)
	}
	if !sawResult {
		t.Errorf("expected server_tool_result part, got %+v", parts)
	}
}

// TestConvertGeminiResponseCodeExecutionResultFailedOutcome verifies the
// IsError flag is set for non-OK outcomes so downstream consumers can
// distinguish failed runs from successful ones.
func TestConvertGeminiResponseCodeExecutionResultFailedOutcome(t *testing.T) {
	reason := "STOP"
	resp := &model.GeminiGenerateContentResponse{
		Candidates: []*model.GeminiCandidate{
			{
				Index:        0,
				FinishReason: &reason,
				Content: &model.GeminiContent{
					Role: "model",
					Parts: []*model.GeminiPart{
						{CodeExecutionResult: &model.GeminiCodeExecutionResult{
							Outcome: "OUTCOME_FAILED",
							Output:  "SyntaxError",
						}},
					},
				},
			},
		},
	}
	internal := convertGeminiToLLMResponse(resp, false, nil)
	parts := internal.Choices[0].Message.Content.MultipleContent
	if len(parts) != 1 || parts[0].ServerToolResult == nil {
		t.Fatalf("expected single server_tool_result part, got %+v", parts)
	}
	if parts[0].ServerToolResult.IsError == nil || !*parts[0].ServerToolResult.IsError {
		t.Errorf("expected IsError=true for OUTCOME_FAILED, got %+v", parts[0].ServerToolResult.IsError)
	}
}

func stringPtr(v string) *string {
	return &v
}

// TestConvertGeminiRequestCachedContentAndLabels verifies G-H8:
//   - InternalLLMRequest.GeminiCachedContentRef populates the top-level
//     `cachedContent` field on the Gemini wire body.
//   - InternalLLMRequest.Metadata is forwarded as `labels` (same k/v
//     semantics on both sides).
//   - Empty / whitespace-only cached-content refs are dropped (wire omits
//     the field entirely thanks to omitempty).
func TestConvertGeminiRequestCachedContentAndLabels(t *testing.T) {
	ref := "cachedContents/abc123"
	req := &model.InternalLLMRequest{
		Model: "gemini-2.5-flash",
		Messages: []model.Message{
			{Role: "user", Content: model.MessageContent{Content: stringPtr("hi")}},
		},
		GeminiCachedContentRef: &ref,
		Metadata: map[string]string{
			"project": "demo",
			"team":    "eng",
		},
	}
	out := convertLLMToGeminiRequest(req)
	if out.CachedContent != ref {
		t.Errorf("expected cachedContent=%q, got %q", ref, out.CachedContent)
	}
	if out.Labels["project"] != "demo" || out.Labels["team"] != "eng" {
		t.Errorf("expected labels to include project/team, got %+v", out.Labels)
	}

	// Whitespace-only ref should drop the field.
	blank := "   "
	req.GeminiCachedContentRef = &blank
	out = convertLLMToGeminiRequest(req)
	if out.CachedContent != "" {
		t.Errorf("expected blank cachedContent to be dropped, got %q", out.CachedContent)
	}

	// Nil ref + nil metadata -> wire body omits both keys.
	req.GeminiCachedContentRef = nil
	req.Metadata = nil
	out = convertLLMToGeminiRequest(req)
	b, err := json.Marshal(out)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	wire := string(b)
	if strings.Contains(wire, `"cachedContent"`) {
		t.Errorf("expected omitempty on cachedContent, wire=%s", wire)
	}
	if strings.Contains(wire, `"labels"`) {
		t.Errorf("expected omitempty on labels, wire=%s", wire)
	}
}

// TestConvertGeminiRequestSystemInstructionWireShape asserts the Gemini
// request JSON uses the camelCase `systemInstruction` key (not snake_case)
// and that the system instruction content omits `role` entirely, matching
// Gemini's REST spec. (G-C3)
// Ref: https://ai.google.dev/api/generate-content#request-body
func TestConvertGeminiRequestSystemInstructionWireShape(t *testing.T) {
	req := &model.InternalLLMRequest{
		Model: "gemini-2.5-flash",
		Messages: []model.Message{
			{Role: "system", Content: model.MessageContent{Content: stringPtr("be concise")}},
			{Role: "user", Content: model.MessageContent{Content: stringPtr("hi")}},
		},
	}
	out := convertLLMToGeminiRequest(req)
	b, err := json.Marshal(out)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	wire := string(b)
	if !strings.Contains(wire, `"systemInstruction":`) {
		t.Errorf("expected camelCase systemInstruction key, got %s", wire)
	}
	if strings.Contains(wire, `"system_instruction"`) {
		t.Errorf("unexpected snake_case key in wire: %s", wire)
	}
	// The systemInstruction body must not carry a role field. We look for
	// `"role":""` specifically; user / model roles are still allowed
	// elsewhere.
	if strings.Contains(wire, `"role":""`) {
		t.Errorf("systemInstruction should omit empty role, wire=%s", wire)
	}
	// Sanity-check the user turn still carries its role.
	if !strings.Contains(wire, `"role":"user"`) {
		t.Errorf("expected user role preserved, wire=%s", wire)
	}
}
