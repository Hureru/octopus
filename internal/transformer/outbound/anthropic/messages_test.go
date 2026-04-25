package anthropic

import (
	"context"
	"encoding/json"
	"io"
	"strings"
	"testing"

	anthropicModel "github.com/bestruirui/octopus/internal/transformer/inbound/anthropic"
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

// TestCollectBetaHeadersAutomation covers A-H7 — each new signal drives a
// specific anthropic-beta header. The test is table-driven so adding a
// future trigger only needs a new row, not a whole test function.
func TestCollectBetaHeadersAutomation(t *testing.T) {
	tt := true
	t.Run("mcp_servers", func(t *testing.T) {
		anthropicReq := &anthropicModel.MessageRequest{
			Model:      "claude-opus-4",
			MaxTokens:  16,
			MCPServers: []byte(`[{"type":"url","url":"x","name":"d"}]`),
		}
		internal := &model.InternalLLMRequest{}
		betas := collectAnthropicBetaHeaders(anthropicReq, internal)
		if !containsBeta(betas, "mcp-client-2025-11-20") {
			t.Errorf("expected mcp-client beta, got %v", betas)
		}
	})

	t.Run("structured_outputs_json_schema", func(t *testing.T) {
		anthropicReq := &anthropicModel.MessageRequest{Model: "claude-sonnet-4-5", MaxTokens: 16}
		internal := &model.InternalLLMRequest{
			ResponseFormat: &model.ResponseFormat{Type: "json_schema"},
		}
		betas := collectAnthropicBetaHeaders(anthropicReq, internal)
		if !containsBeta(betas, "structured-outputs-2025-11-13") {
			t.Errorf("expected structured-outputs beta, got %v", betas)
		}
	})

	t.Run("interleaved_thinking_and_tool_use", func(t *testing.T) {
		tool := "tool_use"
		thought := "thinking"
		txt := "think..."
		anthropicReq := &anthropicModel.MessageRequest{
			Model:     "claude-sonnet-4-5",
			MaxTokens: 16,
			Thinking:  &anthropicModel.Thinking{Type: anthropicModel.ThinkingTypeEnabled},
			Messages: []anthropicModel.MessageParam{
				{Role: "assistant", Content: anthropicModel.MessageContent{
					MultipleContent: []anthropicModel.MessageContentBlock{
						{Type: thought, Thinking: &txt},
						{Type: tool, ID: "call_1"},
					},
				}},
			},
		}
		betas := collectAnthropicBetaHeaders(anthropicReq, &model.InternalLLMRequest{})
		if !containsBeta(betas, "interleaved-thinking-2025-05-14") {
			t.Errorf("expected interleaved-thinking beta, got %v", betas)
		}
	})

	t.Run("context_1m_sonnet4_with_flag", func(t *testing.T) {
		anthropicReq := &anthropicModel.MessageRequest{
			Model:     "claude-sonnet-4-5",
			MaxTokens: 16,
		}
		internal := &model.InternalLLMRequest{
			TransformerMetadata: map[string]string{"anthropic_context_1m": "true"},
		}
		betas := collectAnthropicBetaHeaders(anthropicReq, internal)
		if !containsBeta(betas, "context-1m-2025-08-07") {
			t.Errorf("expected context-1m beta, got %v", betas)
		}

		// Opus 4.6 supports 1M natively — no beta expected even with the flag.
		anthropicReq.Model = "claude-opus-4-6"
		betas = collectAnthropicBetaHeaders(anthropicReq, internal)
		if containsBeta(betas, "context-1m-2025-08-07") {
			t.Errorf("opus 4.6 should not trigger context-1m beta, got %v", betas)
		}
	})

	t.Run("files_api_source", func(t *testing.T) {
		anthropicReq := &anthropicModel.MessageRequest{
			Model: "claude-sonnet-4-5", MaxTokens: 16,
			Messages: []anthropicModel.MessageParam{{
				Role: "user",
				Content: anthropicModel.MessageContent{
					MultipleContent: []anthropicModel.MessageContentBlock{
						{Type: "document", Source: &anthropicModel.ImageSource{
							Type: "file", Data: "file-abc123",
						}},
					},
				},
			}},
		}
		betas := collectAnthropicBetaHeaders(anthropicReq, &model.InternalLLMRequest{})
		if !containsBeta(betas, "files-api-2025-04-14") {
			t.Errorf("expected files-api beta, got %v", betas)
		}
	})

	t.Run("fine_grained_tool_streaming", func(t *testing.T) {
		anthropicReq := &anthropicModel.MessageRequest{
			Model: "claude-sonnet-4-5", MaxTokens: 16,
			Stream: &tt,
			Tools: []anthropicModel.Tool{
				{Name: "search", Description: "find", InputSchema: json.RawMessage(`{"type":"object"}`)},
			},
		}
		betas := collectAnthropicBetaHeaders(anthropicReq, &model.InternalLLMRequest{})
		if !containsBeta(betas, "fine-grained-tool-streaming-2025-05-14") {
			t.Errorf("expected fine-grained-tool-streaming beta, got %v", betas)
		}

		// No tools → no beta.
		anthropicReq.Tools = nil
		betas = collectAnthropicBetaHeaders(anthropicReq, &model.InternalLLMRequest{})
		if containsBeta(betas, "fine-grained-tool-streaming-2025-05-14") {
			t.Errorf("expected no fine-grained beta without tools, got %v", betas)
		}
	})

	t.Run("tool_search_defer_loading", func(t *testing.T) {
		defer1 := true
		anthropicReq := &anthropicModel.MessageRequest{
			Model: "claude-sonnet-4-5", MaxTokens: 16,
			Tools: []anthropicModel.Tool{
				{Name: "big", Description: "lazy", InputSchema: json.RawMessage(`{"type":"object"}`), DeferLoading: &defer1},
			},
		}
		betas := collectAnthropicBetaHeaders(anthropicReq, &model.InternalLLMRequest{})
		if !containsBeta(betas, "tool-search-tool-2025-10-19") {
			t.Errorf("expected tool-search-tool beta, got %v", betas)
		}
	})
}

// containsBeta is a tiny helper for the beta-header table tests.
func containsBeta(betas []string, want string) bool {
	for _, b := range betas {
		if b == want {
			return true
		}
	}
	return false
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

func TestPruneCacheBreakpointsKeepsSystemThenToolsThenMessages(t *testing.T) {
	req := &anthropicModel.MessageRequest{
		System: &anthropicModel.SystemPrompt{
			MultiplePrompts: []anthropicModel.SystemPromptPart{
				{Type: "text", Text: "sys-1", CacheControl: &anthropicModel.CacheControl{Type: model.CacheControlTypeEphemeral}},
				{Type: "text", Text: "sys-2", CacheControl: &anthropicModel.CacheControl{Type: model.CacheControlTypeEphemeral}},
			},
		},
		Tools: []anthropicModel.Tool{
			{Name: "tool-1", CacheControl: &anthropicModel.CacheControl{Type: model.CacheControlTypeEphemeral}},
			{Name: "tool-2", CacheControl: &anthropicModel.CacheControl{Type: model.CacheControlTypeEphemeral}},
		},
		Messages: []anthropicModel.MessageParam{{
			Role: "user",
			Content: anthropicModel.MessageContent{MultipleContent: []anthropicModel.MessageContentBlock{{
				Type: "text", Text: stringPtr("msg-1"), CacheControl: &anthropicModel.CacheControl{Type: model.CacheControlTypeEphemeral},
			}}},
		}},
	}

	pruneCacheBreakpoints(req)

	if req.System.MultiplePrompts[0].CacheControl == nil || req.System.MultiplePrompts[1].CacheControl == nil {
		t.Fatalf("expected system breakpoints to be kept first, got %+v", req.System.MultiplePrompts)
	}
	if req.Tools[0].CacheControl == nil || req.Tools[1].CacheControl == nil {
		t.Fatalf("expected tool breakpoints to be kept before messages, got %+v", req.Tools)
	}
	if req.Messages[0].Content.MultipleContent[0].CacheControl != nil {
		t.Fatalf("expected message breakpoint beyond limit to be pruned, got %+v", req.Messages[0].Content.MultipleContent[0])
	}
}

func TestPruneCacheBreakpointsCountsToolUseAndToolResultContent(t *testing.T) {
	lookup := stringPtr("lookup")
	callID := stringPtr("call_1")
	req := &anthropicModel.MessageRequest{
		Messages: []anthropicModel.MessageParam{{
			Role: "assistant",
			Content: anthropicModel.MessageContent{MultipleContent: []anthropicModel.MessageContentBlock{
				{Type: "text", Text: stringPtr("m1"), CacheControl: &anthropicModel.CacheControl{Type: model.CacheControlTypeEphemeral}},
				{Type: "tool_use", ID: "call_1", Name: lookup, CacheControl: &anthropicModel.CacheControl{Type: model.CacheControlTypeEphemeral}},
				{Type: "tool_result", ToolUseID: callID, CacheControl: &anthropicModel.CacheControl{Type: model.CacheControlTypeEphemeral}},
				{Type: "text", Text: stringPtr("m4"), CacheControl: &anthropicModel.CacheControl{Type: model.CacheControlTypeEphemeral}},
				{Type: "text", Text: stringPtr("m5"), CacheControl: &anthropicModel.CacheControl{Type: model.CacheControlTypeEphemeral}},
			}},
		}},
	}

	pruneCacheBreakpoints(req)
	parts := req.Messages[0].Content.MultipleContent
	for i := 0; i < model.AnthropicMaxCacheBreakpoints; i++ {
		if parts[i].CacheControl == nil {
			t.Fatalf("expected breakpoint %d to be preserved, got %+v", i, parts)
		}
	}
	if parts[4].CacheControl != nil {
		t.Fatalf("expected fifth breakpoint to be pruned, got %+v", parts[4])
	}
}

func stringPtr(v string) *string {
	return &v
}

// TestConvertStopSequencesCapsArrayLength verifies A-L5: arrays longer
// than Anthropic's empirical ceiling are truncated instead of surfacing
// an opaque 400 from the upstream. The single-string form is left
// unchanged.
func TestConvertStopSequencesCapsArrayLength(t *testing.T) {
	// Shorten the cap so the fixture stays readable.
	orig := anthropicMaxStopSequences
	anthropicMaxStopSequences = 2
	defer func() { anthropicMaxStopSequences = orig }()

	stop := &model.Stop{MultipleStop: []string{"a", "b", "c", "d"}}
	seqs := convertStopSequences(stop)
	if len(seqs) != 2 || seqs[0] != "a" || seqs[1] != "b" {
		t.Errorf("expected first 2 entries kept, got %+v", seqs)
	}

	// Short array is untouched.
	stop = &model.Stop{MultipleStop: []string{"only"}}
	if seqs = convertStopSequences(stop); len(seqs) != 1 || seqs[0] != "only" {
		t.Errorf("expected short array unchanged, got %+v", seqs)
	}

	// Single-string form is preserved.
	s := "stop-here"
	stop = &model.Stop{Stop: &s}
	if seqs = convertStopSequences(stop); len(seqs) != 1 || seqs[0] != "stop-here" {
		t.Errorf("expected single-string form preserved, got %+v", seqs)
	}
}

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

func TestConvertToAnthropicRequestDropsUnsupportedCacheControlValues(t *testing.T) {
	req := &model.InternalLLMRequest{
		Model: "claude-3-5-sonnet",
		Messages: []model.Message{{
			Role: "user",
			Content: model.MessageContent{MultipleContent: []model.MessageContentPart{{
				Type:         "text",
				Text:         stringPtr("hello"),
				CacheControl: &model.CacheControl{Type: "future_type", TTL: "future_ttl"},
			}}},
		}},
	}

	out := convertToAnthropicRequest(req)
	if len(out.Messages) != 1 || len(out.Messages[0].Content.MultipleContent) != 1 {
		t.Fatalf("expected one content block, got %+v", out.Messages)
	}
	if got := out.Messages[0].Content.MultipleContent[0].CacheControl; got != nil {
		t.Fatalf("expected unsupported cache_control to be dropped, got %+v", got)
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
						Type:                tc.toolType,
						Function:            model.Function{Name: strings.Split(tc.toolType, "_")[0]},
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
		"":                        "",
		"function":                "",
		"custom":                  "",
		"web_search_20250305":     "web-search-2025-03-05",
		"web_search_20260101":     "web-search-2025-03-05",
		"code_execution_20250522": "code-execution-2025-05-22",
		"computer_20250124":       "computer-use-2025-01-24",
		"something_unknown":       "",
	}
	for in, want := range cases {
		if got := anthropicServerToolBeta(in); got != want {
			t.Fatalf("anthropicServerToolBeta(%q) = %q, want %q", in, got, want)
		}
	}
}
