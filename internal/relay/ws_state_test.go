package relay

import (
	"encoding/json"
	"fmt"
	"net/http"
	"testing"

	transformerModel "github.com/bestruirui/octopus/internal/transformer/model"
)

func TestBuildWSResponseCreateMessageRemovesWSOnlyFields(t *testing.T) {
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
	if _, exists := payload["stream"]; exists {
		t.Fatalf("expected stream field to be removed, got %#v", payload)
	}
	if _, exists := payload["background"]; exists {
		t.Fatalf("expected background field to be removed, got %#v", payload)
	}
	if _, exists := payload["previous_response_id"]; exists {
		t.Fatalf("expected no implicit previous_response_id injection, got %#v", payload)
	}
}

func TestInjectWSPreviousResponseIDOnlyWhenMissing(t *testing.T) {
	reqBody := map[string]json.RawMessage{
		"model": json.RawMessage(`"gpt-4o"`),
	}

	injectWSPreviousResponseID(reqBody, &wsConversationState{LastResponseID: "resp_cached"})
	if got := string(reqBody["previous_response_id"]); got != `"resp_cached"` {
		t.Fatalf("expected cached previous_response_id to be injected, got %s", got)
	}

	reqBody["previous_response_id"] = json.RawMessage(`"resp_explicit"`)
	injectWSPreviousResponseID(reqBody, &wsConversationState{LastResponseID: "resp_cached_2"})
	if got := string(reqBody["previous_response_id"]); got != `"resp_explicit"` {
		t.Fatalf("expected explicit previous_response_id to be preserved, got %s", got)
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
	if publicErr.Code != "conversation_restart_required" {
		t.Fatalf("expected conversation restart code, got %q", publicErr.Code)
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
	if !shouldMarkWSUnsupported(nil, fmt.Errorf("failed handshake: expected handshake response status code 426 but got 426")) {
		t.Fatalf("expected upgrade required handshake to mark ws unsupported")
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

func TestWSConversationStateCanAutoRestart(t *testing.T) {
	state := &wsConversationState{
		LastResponseID: "resp_prev",
		Transcript: []transformerModel.Message{{
			Role: "assistant",
		}},
	}
	if !state.CanAutoRestart(&transformerModel.InternalLLMRequest{PreviousResponseID: stringPtr("resp_prev")}) {
		t.Fatalf("expected latest previous_response_id to be auto-restartable")
	}
	if state.CanAutoRestart(&transformerModel.InternalLLMRequest{PreviousResponseID: stringPtr("resp_other")}) {
		t.Fatalf("expected mismatched previous_response_id to skip auto restart")
	}
}

func TestWSConversationStateBuildReplayRequest(t *testing.T) {
	state := &wsConversationState{
		LastResponseID: "resp_prev",
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
	if len(replayed.Messages) != 2 {
		t.Fatalf("expected replay transcript plus current turn, got %d messages", len(replayed.Messages))
	}
	if requiresUpstreamWSContinuation(replayed) {
		t.Fatalf("expected replay request to be self-contained")
	}
	var rawItems []map[string]any
	if err := json.Unmarshal(replayed.RawInputItems, &rawItems); err != nil {
		t.Fatalf("expected replay raw input items to be valid json, got %v", err)
	}
	if len(rawItems) != 3 {
		t.Fatalf("expected transcript item plus original raw items, got %d items", len(rawItems))
	}
	if rawItems[0]["type"] != "function_call" {
		t.Fatalf("expected transcript assistant tool call to be preserved, got %#v", rawItems[0])
	}
	if _, ok := rawItems[2]["native_meta"]; !ok {
		t.Fatalf("expected original raw input item native fields to be preserved, got %#v", rawItems[2])
	}
}

func TestWSConversationStateApplySuccessfulTurn(t *testing.T) {
	state := &wsConversationState{}
	request := &transformerModel.InternalLLMRequest{
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
				Role:    "assistant",
				Content: transformerModel.MessageContent{Content: stringPtr("hi")},
			},
		}},
	}

	state.ApplySuccessfulTurn(request, response)
	if state.LastResponseID != "resp_new" {
		t.Fatalf("expected last response id to be updated, got %q", state.LastResponseID)
	}
	if len(state.Transcript) != 2 {
		t.Fatalf("expected transcript to contain request and response, got %d messages", len(state.Transcript))
	}
}

func stringPtr(value string) *string {
	return &value
}
