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

	injectWSPreviousResponseID(reqBody, &cachedWSResponse{ID: "resp_cached"})
	if got := string(reqBody["previous_response_id"]); got != `"resp_cached"` {
		t.Fatalf("expected cached previous_response_id to be injected, got %s", got)
	}

	reqBody["previous_response_id"] = json.RawMessage(`"resp_explicit"`)
	injectWSPreviousResponseID(reqBody, &cachedWSResponse{ID: "resp_cached_2"})
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
	if requiresUpstreamWSContinuation(&transformerModel.InternalLLMRequest{Messages: []transformerModel.Message{{Role: "user"}}}) {
		t.Fatalf("expected ordinary request to not require upstream ws continuation")
	}
}

func stringPtr(value string) *string {
	return &value
}
