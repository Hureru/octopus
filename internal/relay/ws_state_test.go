package relay

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"

	transformerModel "github.com/bestruirui/octopus/internal/transformer/model"
)

func TestBuildWSResponseCreateMessageNormalizesWSFields(t *testing.T) {
	message, err := buildWSResponseCreateMessage(json.RawMessage(`{"model":"gpt-4o","stream":true,"background":true}`))
	if err != nil {
		t.Fatalf("buildWSResponseCreateMessage failed: %v", err)
	}

	var payload map[string]json.RawMessage
	if err := json.Unmarshal(message, &payload); err != nil {
		t.Fatalf("unmarshal merged message failed: %v", err)
	}

	if got := string(payload["type"]); got != `"response.create"` {
		t.Fatalf("expected type response.create, got %s", got)
	}
	if got := string(payload["stream"]); got != `true` {
		t.Fatalf("expected stream field to be forced true, got %s in %#v", got, payload)
	}
	if _, exists := payload["background"]; exists {
		t.Fatalf("expected background field to be removed, got %#v", payload)
	}
	if _, exists := payload["previous_response_id"]; exists {
		t.Fatalf("expected no implicit previous_response_id injection, got %#v", payload)
	}
}

func TestWSRequestExplicitlyRequestsContinuation(t *testing.T) {
	if wsRequestExplicitlyRequestsContinuation(nil) {
		t.Fatalf("expected nil request body to be fresh")
	}
	if wsRequestExplicitlyRequestsContinuation(map[string]json.RawMessage{"previous_response_id": json.RawMessage(`""`)}) {
		t.Fatalf("expected empty previous_response_id to be treated as fresh")
	}
	if !wsRequestExplicitlyRequestsContinuation(map[string]json.RawMessage{"previous_response_id": json.RawMessage(`"resp_prev"`)}) {
		t.Fatalf("expected previous_response_id to request continuation")
	}
	if !wsRequestExplicitlyRequestsContinuation(map[string]json.RawMessage{"conversation": json.RawMessage(`{"id":"conv_1"}`)}) {
		t.Fatalf("expected conversation payload to request continuation")
	}
}

func TestClassifyWSPublicErrorRecognizesConversationRestart(t *testing.T) {
	err := fmt.Errorf("ws stream read error: ws read error: failed to get reader: received close frame: status = StatusPolicyViolation and reason = \"upstream continuation connection is unavailable; please restart the conversation\"")
	publicErr, ok := classifyWSPublicError(err, http.StatusConflict)
	if !ok {
		t.Fatalf("expected conversation restart error to be classified")
	}
	if publicErr.Status != http.StatusConflict {
		t.Fatalf("expected conflict status, got %d", publicErr.Status)
	}
	if !publicErr.ResetConversation {
		t.Fatalf("expected conversation restart error to reset cached conversation")
	}
	if publicErr.Code != "previous_response_not_found" {
		t.Fatalf("expected the code clients resend in full on, got %q", publicErr.Code)
	}
}

func TestClassifyWSPublicErrorRecognizesNoAvailableAccount(t *testing.T) {
	err := fmt.Errorf("ws stream read error: ws read error: failed to get reader: received close frame: status = StatusTryAgainLater and reason = \"no available account\"")
	publicErr, ok := classifyWSPublicError(err, http.StatusServiceUnavailable)
	if !ok {
		t.Fatalf("expected no available account error to be classified")
	}
	if publicErr.Status != http.StatusServiceUnavailable {
		t.Fatalf("expected service unavailable status, got %d", publicErr.Status)
	}
	if publicErr.ResetConversation {
		t.Fatalf("expected no available account error to keep cached conversation")
	}
}

func TestNormalizeUpstreamStatusCode(t *testing.T) {
	if got := normalizeUpstreamStatusCode(http.StatusInternalServerError, `{"error":{"message":"blocked_invalid_request: request body matches a previously blocked invalid request"}}`); got != http.StatusBadRequest {
		t.Fatalf("expected blocked invalid request to become 400, got %d", got)
	}
	if got := normalizeUpstreamStatusCode(http.StatusBadRequest, `{"error":{"message":"No tool call found for function call output with call_id fc_xxx"}}`); got != http.StatusConflict {
		t.Fatalf("expected missing tool call to become 409, got %d", got)
	}
}

func TestShouldMarkWSUnsupported(t *testing.T) {
	if !shouldMarkWSUnsupported(&http.Response{StatusCode: http.StatusNotFound}, fmt.Errorf("bad handshake")) {
		t.Fatalf("expected 404 handshake to mark ws unsupported")
	}
	if shouldMarkWSUnsupported(&http.Response{StatusCode: http.StatusServiceUnavailable}, fmt.Errorf("temporary upstream unavailable")) {
		t.Fatalf("expected 503 handshake to remain retryable")
	}
	if shouldMarkWSUnsupported(nil, fmt.Errorf("failed handshake: expected handshake response status code 426 but got 426")) {
		t.Fatalf("expected upgrade required handshake to remain retryable")
	}
}

func TestIsUpstreamWSConnectionBroken(t *testing.T) {
	for _, err := range []error{
		fmt.Errorf("failed to write frame payload: write: broken pipe"),
		fmt.Errorf("failed to get reader: failed to read frame header: EOF"),
		fmt.Errorf("use of closed network connection"),
	} {
		if !isUpstreamWSConnectionBroken(err) {
			t.Fatalf("expected %q to be treated as broken upstream ws", err)
		}
	}
	if isUpstreamWSConnectionBroken(fmt.Errorf("temporary upstream unavailable")) {
		t.Fatalf("expected unrelated error to not be treated as broken upstream ws")
	}
}

func TestRequiresUpstreamWSContinuation(t *testing.T) {
	if !requiresUpstreamWSContinuation(&transformerModel.InternalLLMRequest{PreviousResponseID: stringPtr("resp_123")}) {
		t.Fatalf("expected previous_response_id request to require upstream ws continuation")
	}
	if !requiresUpstreamWSContinuation(&transformerModel.InternalLLMRequest{Messages: []transformerModel.Message{{Role: "tool", ToolCallID: stringPtr("call_123")}}}) {
		t.Fatalf("expected tool output request to require upstream ws continuation")
	}
	replayable := &transformerModel.InternalLLMRequest{Messages: []transformerModel.Message{
		{
			Role: "assistant",
			ToolCalls: []transformerModel.ToolCall{{
				ID:   "call_123",
				Type: "function",
				Function: transformerModel.FunctionCall{
					Name: "lookup",
				},
			}},
		},
		{
			Role:       "tool",
			ToolCallID: stringPtr("call_123"),
		},
	}}
	if requiresUpstreamWSContinuation(replayable) {
		t.Fatalf("expected replayable transcript to not require upstream ws continuation")
	}
	if requiresUpstreamWSContinuation(&transformerModel.InternalLLMRequest{Messages: []transformerModel.Message{{Role: "user"}}}) {
		t.Fatalf("expected ordinary request to not require upstream ws continuation")
	}
}

func TestWSConversationStateBuildReplayRequest(t *testing.T) {
	state := &wsConversationState{
		LastResponseID: "resp_prev",
		ReplayWindowItems: json.RawMessage(`[
			{"type":"function_call","call_id":"call_123","name":"lookup","arguments":"{}"}
		]`),
		Transcript: []transformerModel.Message{{
			Role: "assistant",
			ToolCalls: []transformerModel.ToolCall{{
				ID:   "call_123",
				Type: "function",
				Function: transformerModel.FunctionCall{
					Name:      "lookup",
					Arguments: `{}`,
				},
			}},
		}},
	}
	req := &transformerModel.InternalLLMRequest{
		Model:              "gpt-4o",
		PreviousResponseID: stringPtr("resp_prev"),
		RawInputItems: json.RawMessage(`[
			{"type":"function_call_output","call_id":"call_123","output":[{"type":"input_text","text":"ok"}]},
			{"type":"input_text","text":"tail","native_meta":{"keep":true}}
		]`),
		ProviderExtensions: &transformerModel.ProviderExtensions{
			OpenAI: &transformerModel.OpenAIExtension{
				RawResponseItems: json.RawMessage(`[{"type":"input_text","text":"stale"}]`),
			},
		},
		Messages: []transformerModel.Message{{
			Role:       "tool",
			ToolCallID: stringPtr("call_123"),
			Content: transformerModel.MessageContent{
				Content: stringPtr("ok"),
			},
		}},
	}

	replayed := state.BuildReplayRequest(req)
	if replayed == nil {
		t.Fatalf("expected replay request to be built")
	}
	if replayed.PreviousResponseID != nil {
		t.Fatalf("expected replay request to clear previous_response_id")
	}
	if replayed.Conversation != nil {
		t.Fatalf("expected replay request to clear conversation state")
	}
	if len(replayed.Messages) != 2 || replayed.Messages[0].Role != "assistant" || replayed.Messages[1].Role != "tool" {
		t.Fatalf("expected replay messages to carry the stored turn and the new input, got %#v", replayed.Messages)
	}
	if replayed.TransformOptions.ArrayInputs == nil || !*replayed.TransformOptions.ArrayInputs {
		t.Fatalf("expected replay request to force array input semantics")
	}
	if replayed.TransformerMetadata[transformerModel.TransformerMetadataWSExecutionMode] != transformerModel.TransformerMetadataWSExecutionModeReplayExact {
		t.Fatalf("expected replay request to be marked replay_exact, got %#v", replayed.TransformerMetadata)
	}
	if requiresUpstreamWSContinuation(replayed) {
		t.Fatalf("expected replay request to be self-contained")
	}
	var rawItems []map[string]any
	if err := json.Unmarshal(replayed.RawInputItems, &rawItems); err != nil {
		t.Fatalf("expected replay raw input items to be valid json, got %v", err)
	}
	if len(rawItems) != 3 {
		t.Fatalf("expected replay window plus original raw items, got %d items", len(rawItems))
	}
	if rawItems[0]["type"] != "function_call" {
		t.Fatalf("expected replay window tool call to be preserved, got %#v", rawItems[0])
	}
	if _, ok := rawItems[2]["native_meta"]; !ok {
		t.Fatalf("expected original raw input item native fields to be preserved, got %#v", rawItems[2])
	}
	if replayed.ProviderExtensions == nil || replayed.ProviderExtensions.OpenAI == nil {
		t.Fatalf("expected replay request to keep OpenAI mirror in sync")
	}
	if string(replayed.ProviderExtensions.OpenAI.RawResponseItems) != string(replayed.RawInputItems) {
		t.Fatalf("expected replayed OpenAI mirror to match authoritative RawInputItems, got mirror=%s raw=%s", replayed.ProviderExtensions.OpenAI.RawResponseItems, replayed.RawInputItems)
	}
	if req.ProviderExtensions != nil && req.ProviderExtensions.OpenAI != nil && string(req.ProviderExtensions.OpenAI.RawResponseItems) == string(replayed.RawInputItems) {
		t.Fatalf("expected replay build to avoid mutating original request extension mirror")
	}
}

func TestWSConversationStateBuildReplayRequestForToolOutput(t *testing.T) {
	state := &wsConversationState{
		LastResponseID: "resp_replayed",
		ReplayWindowItems: json.RawMessage(`[
			{"type":"function_call","call_id":"call_123","name":"lookup","arguments":"{}"}
		]`),
		Transcript: []transformerModel.Message{
			{
				Role: "assistant",
				ToolCalls: []transformerModel.ToolCall{{
					ID:   "call_123",
					Type: "function",
					Function: transformerModel.FunctionCall{
						Name:      "lookup",
						Arguments: `{}`,
					},
				}},
			},
		},
	}
	req := &transformerModel.InternalLLMRequest{
		Model:              "gpt-4o",
		PreviousResponseID: stringPtr("resp_replayed"),
		RawInputItems: json.RawMessage(`[
			{"type":"function_call_output","call_id":"call_123","output":"ok"}
		]`),
		Messages: []transformerModel.Message{{
			Role:       "tool",
			ToolCallID: stringPtr("call_123"),
			Content: transformerModel.MessageContent{
				Content: stringPtr("ok"),
			},
		}},
	}

	replayed := state.BuildReplayRequest(req)
	if replayed == nil {
		t.Fatalf("expected replay request to be built")
	}
	if replayed.PreviousResponseID != nil {
		t.Fatalf("expected replay request to clear previous_response_id")
	}
	if len(replayed.Messages) != 2 || replayed.Messages[0].Role != "assistant" || replayed.Messages[1].Role != "tool" {
		t.Fatalf("expected replay messages to carry the stored turn and the new input, got %#v", replayed.Messages)
	}
	if replayed.TransformerMetadata[transformerModel.TransformerMetadataWSExecutionMode] != transformerModel.TransformerMetadataWSExecutionModeReplayExact {
		t.Fatalf("expected replay-pending request to be marked replay_exact, got %#v", replayed.TransformerMetadata)
	}
}

func TestWSConversationStateApplySuccessfulTurn(t *testing.T) {
	state := &wsConversationState{}
	request := &transformerModel.InternalLLMRequest{
		Model: "gpt-4o",
		Messages: []transformerModel.Message{{
			Role:    "user",
			Content: transformerModel.MessageContent{Content: stringPtr("hello")},
		}},
	}
	response := &transformerModel.InternalLLMResponse{
		ID: "resp_new",
		Choices: []transformerModel.Choice{{
			Index: 0,
			Message: &transformerModel.Message{
				Role:               "assistant",
				Content:            transformerModel.MessageContent{Content: stringPtr("hi")},
				ReasoningContent:   stringPtr("thinking"),
				ReasoningSignature: stringPtr("signature of another provider"),
			},
		}},
	}

	state.ApplySuccessfulTurn(request, response)
	if state.LastResponseID != "resp_new" {
		t.Fatalf("expected last response id to be updated, got %q", state.LastResponseID)
	}
	if state.RequestModel != "gpt-4o" {
		t.Fatalf("expected request model to be remembered, got %q", state.RequestModel)
	}
	if len(state.ReplayWindowItems) == 0 {
		t.Fatalf("expected exact replay window to be built on success")
	}
	if len(state.Transcript) != 2 {
		t.Fatalf("expected transcript to contain request and response, got %d messages", len(state.Transcript))
	}
	// 这个回复没有原始输出条目（非 Responses 上游）：回放窗口里也要有模型的回复，但不带别家的推理签名
	var windowItems []map[string]any
	if err := json.Unmarshal(state.ReplayWindowItems, &windowItems); err != nil || len(windowItems) != 2 || windowItems[1]["role"] != "assistant" {
		t.Fatalf("expected the replay window to hold the input and the reply without reasoning, got %s", state.ReplayWindowItems)
	}
	if state.Transcript[1].ReasoningContent == nil {
		t.Fatalf("expected the transcript to keep the reasoning for channels of the same provider")
	}
}

func TestWSConversationStateApplySuccessfulStreamTurnBuildsReplayWindow(t *testing.T) {
	state := &wsConversationState{}
	request := &transformerModel.InternalLLMRequest{
		Model: "gpt-4o",
		Messages: []transformerModel.Message{{
			Role:    "user",
			Content: transformerModel.MessageContent{Content: stringPtr("hello")},
		}},
	}
	events := []transformerModel.StreamEvent{
		{Kind: transformerModel.StreamEventKindMessageStart, ID: "resp_stream", Model: "gpt-4o", Role: "assistant"},
		{Kind: transformerModel.StreamEventKindToolCallStart, ID: "resp_stream", Model: "gpt-4o", Index: 0, ToolCall: &transformerModel.ToolCall{
			ID:    "call_123",
			Type:  "function",
			Index: 0,
			Function: transformerModel.FunctionCall{
				Name: "lookup",
			},
		}},
		{Kind: transformerModel.StreamEventKindMessageStop, ID: "resp_stream", Model: "gpt-4o", StopReason: transformerModel.FinishReasonToolCalls, ProviderExtensions: &transformerModel.ProviderExtensions{
			OpenAI: &transformerModel.OpenAIExtension{
				RawResponseItems: json.RawMessage(`[
					{"type":"function_call","call_id":"call_123","name":"lookup","arguments":"{}","status":"completed"}
				]`),
			},
		}},
		{Kind: transformerModel.StreamEventKindUsageDelta, ID: "resp_stream", Model: "gpt-4o", Usage: &transformerModel.Usage{PromptTokens: 1, CompletionTokens: 1, TotalTokens: 2}, ProviderExtensions: &transformerModel.ProviderExtensions{
			OpenAI: &transformerModel.OpenAIExtension{
				RawResponseItems: json.RawMessage(`[
					{"type":"function_call","call_id":"call_123","name":"lookup","arguments":"{}","status":"completed"}
				]`),
			},
		}},
	}
	response := transformerModel.InternalResponseFromStreamEvents(events)
	if response == nil {
		t.Fatalf("expected streamed events to rebuild response")
	}

	state.ApplySuccessfulTurn(request, response)

	nextReq := &transformerModel.InternalLLMRequest{
		Model:              "gpt-4o",
		PreviousResponseID: stringPtr("resp_stream"),
		RawInputItems: json.RawMessage(`[
			{"type":"function_call_output","call_id":"call_123","output":"ok","native_meta":{"keep":true}}
		]`),
		Messages: []transformerModel.Message{{
			Role:       "tool",
			ToolCallID: stringPtr("call_123"),
			Content:    transformerModel.MessageContent{Content: stringPtr("ok")},
		}},
	}
	replayed := state.BuildReplayRequest(nextReq)
	if replayed == nil {
		t.Fatalf("expected replay request to be built from streamed turn")
	}
	var rawItems []map[string]any
	if err := json.Unmarshal(replayed.RawInputItems, &rawItems); err != nil {
		t.Fatalf("expected replay raw input items to be valid json, got %v", err)
	}
	if len(rawItems) != 3 {
		t.Fatalf("expected original prompt, streamed replay window, and tool output, got %d items", len(rawItems))
	}
	if rawItems[1]["type"] != "function_call" || rawItems[2]["type"] != "function_call_output" {
		t.Fatalf("expected replay order to preserve streamed output then tool result after original prompt, got %#v", rawItems)
	}
	if _, ok := rawItems[2]["native_meta"]; !ok {
		t.Fatalf("expected tool output native fields to survive replay merge, got %#v", rawItems[2])
	}
}

// 非 Responses 渠道从 Messages 生成请求：回放请求的 Messages 是这一轮的说明加完整对话。
func TestBuildReplayRequestMessagesCarryConversation(t *testing.T) {
	text := func(role, content string) transformerModel.Message {
		return transformerModel.Message{Role: role, Content: transformerModel.MessageContent{Content: stringPtr(content)}}
	}
	state := &wsConversationState{
		LastResponseID:    "resp_prev",
		ReplayWindowItems: json.RawMessage(`[{"type":"message","role":"user","content":[{"type":"input_text","text":"hello"}]}]`),
		Transcript:        []transformerModel.Message{text("system", "old instructions"), text("user", "hello"), text("assistant", "hi")},
	}
	req := &transformerModel.InternalLLMRequest{
		Model:              "gpt-4o",
		PreviousResponseID: stringPtr("resp_prev"),
		Messages:           []transformerModel.Message{text("system", "new instructions"), text("developer", "keep developer"), text("user", "again")},
	}

	replayed := state.BuildReplayRequest(req)
	if replayed == nil {
		t.Fatalf("expected replay request to be built")
	}
	var got []string
	for _, msg := range replayed.Messages {
		got = append(got, msg.Role+":"+*msg.Content.Content)
	}
	want := "system:new instructions | developer:keep developer | user:hello | assistant:hi | user:again"
	if strings.Join(got, " | ") != want {
		t.Fatalf("replay messages = %q, want %q", strings.Join(got, " | "), want)
	}
}

func stringPtr(value string) *string {
	return &value
}
