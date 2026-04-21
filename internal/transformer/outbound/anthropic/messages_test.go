package anthropic

import (
	"context"
	"encoding/json"
	"io"
	"strings"
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

// TestConvertToAnthropicRequestForwardsMCPServersAndContainer verifies A-H6:
// the raw mcp_servers and container payloads captured by inbound are
// written back on the outbound request verbatim. Both fields are opaque
// JSON in MessageRequest so the bytes are expected to round-trip with
// no per-field rewriting.
func TestConvertToAnthropicRequestForwardsMCPServersAndContainer(t *testing.T) {
	mcp := []byte(`[{"type":"url","url":"https://example.invalid/mcp","name":"demo","authorization_token":"sk-test"}]`)
	container := []byte(`{"id":"cntr-1","env":{"PYTHONPATH":"/app"}}`)
	req := &model.InternalLLMRequest{
		Model: "claude-opus-4",
		Messages: []model.Message{
			{Role: "user", Content: model.MessageContent{Content: stringPtr("hi")}},
		},
		AnthropicMCPServers: mcp,
		AnthropicContainer:  container,
	}
	out := convertToAnthropicRequest(req)
	if string(out.MCPServers) != string(mcp) {
		t.Errorf("mcp_servers roundtrip: got %s, want %s", out.MCPServers, mcp)
	}
	if string(out.Container) != string(container) {
		t.Errorf("container roundtrip: got %s, want %s", out.Container, container)
	}

	// Independence check: mutating the inbound slice after conversion must
	// not affect the outbound body. The `append(x[:0], src...)` copy
	// pattern we used guarantees this.
	mcp[0] = 'X'
	if out.MCPServers[0] == 'X' {
		t.Errorf("outbound MCPServers aliased the inbound slice (should be a copy)")
	}
}

// TestTransformRequestAddsExtendedCacheTTLBetaHeader verifies that when any
// cache_control.ttl="1h" breakpoint is present anywhere in the outbound
// payload, the `anthropic-beta: extended-cache-ttl-2025-04-11` header is
// attached; Anthropic responds with 400 invalid_request_error otherwise.
// (A-C3) Ref: https://docs.anthropic.com/en/docs/build-with-claude/prompt-caching
func TestTransformRequestAddsExtendedCacheTTLBetaHeader(t *testing.T) {
	outbound := &MessageOutbound{}
	cc1h := &model.CacheControl{Type: model.CacheControlTypeEphemeral, TTL: model.CacheTTL1h}
	req := &model.InternalLLMRequest{
		Model: "claude-3-5-sonnet",
		Messages: []model.Message{
			{
				Role: "user",
				Content: model.MessageContent{
					MultipleContent: []model.MessageContentPart{
						{Type: "text", Text: stringPtr("hello"), CacheControl: cc1h},
					},
				},
			},
		},
	}
	httpReq, err := outbound.TransformRequest(context.Background(), req, "https://api.anthropic.com", "sk-test")
	if err != nil {
		t.Fatalf("TransformRequest: %v", err)
	}
	if got := httpReq.Header.Get("anthropic-beta"); !strings.Contains(got, "extended-cache-ttl-2025-04-11") {
		t.Fatalf("expected extended-cache-ttl beta header, got %q", got)
	}
}

// TestTransformRequestSkipsBetaWhenNoLongTTL ensures we do not attach the
// beta header (which changes Anthropic's billing behaviour) when the
// request only uses default 5m breakpoints or no caching at all.
func TestTransformRequestSkipsBetaWhenNoLongTTL(t *testing.T) {
	outbound := &MessageOutbound{}
	cc5m := &model.CacheControl{Type: model.CacheControlTypeEphemeral, TTL: model.CacheTTL5m}
	req := &model.InternalLLMRequest{
		Model: "claude-3-5-sonnet",
		Messages: []model.Message{
			{
				Role: "user",
				Content: model.MessageContent{
					MultipleContent: []model.MessageContentPart{
						{Type: "text", Text: stringPtr("hello"), CacheControl: cc5m},
					},
				},
			},
		},
	}
	httpReq, err := outbound.TransformRequest(context.Background(), req, "https://api.anthropic.com", "sk-test")
	if err != nil {
		t.Fatalf("TransformRequest: %v", err)
	}
	if got := httpReq.Header.Get("anthropic-beta"); got != "" {
		t.Fatalf("did not expect beta header for 5m TTL, got %q", got)
	}
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

// A-C2: Anthropic streaming "error" event must be surfaced via
// InternalLLMResponse.Error with a reasonable HTTP status mapping, instead of
// being swallowed by the default branch. Reference:
// https://docs.anthropic.com/en/api/messages-streaming#error-events
func TestTransformStreamErrorEventSurfacesResponseError(t *testing.T) {
	cases := []struct {
		name       string
		payload    string
		wantStatus int
		wantType   string
	}{
		{
			name:       "overloaded",
			payload:    `{"type":"error","error":{"type":"overloaded_error","message":"Overloaded"}}`,
			wantStatus: 529,
			wantType:   "overloaded_error",
		},
		{
			name:       "invalid_request",
			payload:    `{"type":"error","error":{"type":"invalid_request_error","message":"bad"}}`,
			wantStatus: 400,
			wantType:   "invalid_request_error",
		},
		{
			name:       "rate_limit",
			payload:    `{"type":"error","error":{"type":"rate_limit_error","message":"slow down"}}`,
			wantStatus: 429,
			wantType:   "rate_limit_error",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			o := &MessageOutbound{}
			resp, err := o.TransformStream(context.Background(), []byte(tc.payload))
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if resp == nil || resp.Error == nil {
				t.Fatalf("expected non-nil InternalLLMResponse.Error, got %+v", resp)
			}
			if resp.Error.StatusCode != tc.wantStatus {
				t.Fatalf("status want=%d got=%d", tc.wantStatus, resp.Error.StatusCode)
			}
			if resp.Error.Detail.Type != tc.wantType {
				t.Fatalf("type want=%q got=%q", tc.wantType, resp.Error.Detail.Type)
			}
			if len(resp.Choices) != 0 {
				t.Fatalf("expected no choices on error chunk, got %d", len(resp.Choices))
			}
		})
	}
}

// A-H5: Anthropic server tools (web_search_*, code_execution_*, computer_*)
// must round-trip through inbound → internal → outbound without dropping the
// spec-specific fields, and the matching `anthropic-beta` header must be
// attached. Previously convertTools explicitly skipped server tools, so
// clients lost access to web search / code execution when routing through us.
func TestTransformRequestPreservesServerToolSpecAndBeta(t *testing.T) {
	cases := []struct {
		name     string
		toolType string
		wantBeta string
		rawSpec  string
	}{
		{
			name:     "web_search",
			toolType: "web_search_20250305",
			wantBeta: "web-search-2025-03-05",
			rawSpec:  `{"type":"web_search_20250305","name":"web_search","max_uses":5,"allowed_domains":["wikipedia.org"]}`,
		},
		{
			name:     "code_execution",
			toolType: "code_execution_20250522",
			wantBeta: "code-execution-2025-05-22",
			rawSpec:  `{"type":"code_execution_20250522","name":"code_execution"}`,
		},
		{
			name:     "computer_use",
			toolType: "computer_20250124",
			wantBeta: "computer-use-2025-01-24",
			rawSpec:  `{"type":"computer_20250124","name":"computer","display_width_px":1024,"display_height_px":768,"display_number":1}`,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			outbound := &MessageOutbound{}
			req := &model.InternalLLMRequest{
				Model: "claude-3-5-sonnet",
				Messages: []model.Message{
					{
						Role:    "user",
						Content: model.MessageContent{Content: stringPtr("hi")},
					},
				},
				Tools: []model.Tool{
					{
						Type: tc.toolType,
						Function: model.Function{Name: strings.Split(tc.toolType, "_")[0]},
						AnthropicServerSpec: json.RawMessage(tc.rawSpec),
					},
				},
			}
			httpReq, err := outbound.TransformRequest(context.Background(), req, "https://api.anthropic.com", "sk-test")
			if err != nil {
				t.Fatalf("TransformRequest: %v", err)
			}
			if got := httpReq.Header.Get("anthropic-beta"); !strings.Contains(got, tc.wantBeta) {
				t.Fatalf("expected %q in anthropic-beta header, got %q", tc.wantBeta, got)
			}

			body, err := io.ReadAll(httpReq.Body)
			if err != nil {
				t.Fatalf("read body: %v", err)
			}
			if !strings.Contains(string(body), tc.toolType) {
				t.Fatalf("expected serialized request to contain %q, got %s", tc.toolType, string(body))
			}
			// Spot-check a spec-specific field survives.
			if tc.name == "web_search" && !strings.Contains(string(body), "allowed_domains") {
				t.Fatalf("expected allowed_domains to be preserved, got %s", string(body))
			}
			if tc.name == "computer_use" && !strings.Contains(string(body), "display_width_px") {
				t.Fatalf("expected display_width_px to be preserved, got %s", string(body))
			}
		})
	}
}

// A-H5: convertTools drops server tools that lack a raw spec payload instead
// of emitting a malformed wire object.
func TestConvertToolsDropsServerToolWithoutSpec(t *testing.T) {
	tools := []model.Tool{
		{
			Type:     "web_search_20250305",
			Function: model.Function{Name: "web_search"},
		},
	}
	got := convertTools(tools)
	if len(got) != 0 {
		t.Fatalf("expected empty result for spec-less server tool, got %+v", got)
	}
}

// A-H5: anthropicServerToolBeta recognises each supported family prefix.
func TestAnthropicServerToolBeta(t *testing.T) {
	cases := map[string]string{
		"":                          "",
		"function":                  "",
		"custom":                    "",
		"web_search_20250305":       "web-search-2025-03-05",
		"web_search_20260101":       "web-search-2025-03-05",
		"code_execution_20250522":   "code-execution-2025-05-22",
		"computer_20250124":         "computer-use-2025-01-24",
		"something_unknown":         "",
	}
	for in, want := range cases {
		if got := anthropicServerToolBeta(in); got != want {
			t.Fatalf("anthropicServerToolBeta(%q) = %q, want %q", in, got, want)
		}
	}
}
