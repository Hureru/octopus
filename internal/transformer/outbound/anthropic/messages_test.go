package anthropic

import (
	"context"
	"encoding/json"
	"io"
	"testing"

	"github.com/bestruirui/octopus/internal/transformer/model"
)

func TestTransformRequestRawRewritesModel(t *testing.T) {
	outbound := &MessageOutbound{}
	rawBody := []byte(`{
		"model":"internal-alias",
		"max_tokens":16,
		"messages":[{"role":"user","content":"hello"}],
		"metadata":{"user_id":"user-123"},
		"custom_flag":true
	}`)

	req, err := outbound.TransformRequestRaw(
		context.Background(),
		rawBody,
		"claude-3-5-sonnet-20241022",
		"https://example.com/v1",
		"test-key",
		nil,
	)
	if err != nil {
		t.Fatalf("TransformRequestRaw() error = %v", err)
	}

	body, err := io.ReadAll(req.Body)
	if err != nil {
		t.Fatalf("ReadAll(req.Body) error = %v", err)
	}

	var payload map[string]any
	if err := json.Unmarshal(body, &payload); err != nil {
		t.Fatalf("unmarshal rewritten body error = %v", err)
	}
	if got := payload["model"]; got != "claude-3-5-sonnet-20241022" {
		t.Fatalf("expected rewritten model, got %#v", got)
	}
	if got := payload["custom_flag"]; got != true {
		t.Fatalf("expected custom fields to survive rewrite, got %#v", got)
	}
}

func TestConvertToAnthropicRequestUsesUserFallbackForMetadata(t *testing.T) {
	req := &model.InternalLLMRequest{
		Model: "claude-3-5-sonnet",
		User:  stringPtr("user-456"),
		Messages: []model.Message{
			{
				Role: "user",
				Content: model.MessageContent{
					Content: stringPtr("hello"),
				},
			},
		},
	}

	anthropicReq := convertToAnthropicRequest(req)
	if anthropicReq.Metadata == nil || anthropicReq.Metadata.UserID != "user-456" {
		t.Fatalf("expected anthropic metadata user_id to use internal user fallback, got %+v", anthropicReq.Metadata)
	}
}

func stringPtr(v string) *string {
	return &v
}

// TestConvertSingleMessageServerToolResultWireType verifies that the
// outbound layer re-emits a code_execution_tool_result block as itself, not
// as web_search_tool_result. Prior implementation looked up
// part.ServerToolUse.Name to decide the wire type, but server_tool_result
// parts never carry a ServerToolUse, so the check was dead code and every
// block became web_search_tool_result — producing mislabelled payloads when
// a code_execution turn round-tripped through the internal model. (A-C1)
func TestConvertSingleMessageServerToolResultWireType(t *testing.T) {
	contentArr, _ := json.Marshal([]map[string]any{{"type": "text", "text": "42"}})
	blockTypeCases := []struct {
		name     string
		inBlock  string
		wantWire string
	}{
		{"code_execution preserved", "code_execution_tool_result", "code_execution_tool_result"},
		{"web_search preserved", "web_search_tool_result", "web_search_tool_result"},
		{"legacy fallback", "", "web_search_tool_result"},
	}
	for _, tc := range blockTypeCases {
		t.Run(tc.name, func(t *testing.T) {
			req := &model.InternalLLMRequest{
				Model: "claude-3-5-sonnet",
				Messages: []model.Message{
					{
						Role: "user",
						Content: model.MessageContent{
							Content: stringPtr("run 6*7"),
						},
					},
					{
						Role: "assistant",
						Content: model.MessageContent{
							MultipleContent: []model.MessageContentPart{
								{
									Type: "server_tool_result",
									ServerToolResult: &model.ServerToolResultBlock{
										ToolUseID: "srvtoolu_abc",
										Content:   contentArr,
										BlockType: tc.inBlock,
									},
								},
							},
						},
					},
				},
			}
			out := convertToAnthropicRequest(req)
			if len(out.Messages) < 2 {
				t.Fatalf("expected user + assistant messages, got %d", len(out.Messages))
			}
			assistantMsg := out.Messages[len(out.Messages)-1]
			if len(assistantMsg.Content.MultipleContent) == 0 {
				t.Fatalf("expected content blocks on assistant message, got %+v", assistantMsg)
			}
			got := assistantMsg.Content.MultipleContent[0].Type
			if got != tc.wantWire {
				t.Fatalf("want wireType=%q, got %q", tc.wantWire, got)
			}
		})
	}
}
