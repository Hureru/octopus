package relay

import (
	"encoding/json"
	"net/http"
	"testing"
)

func TestParseWSUpstreamEventParsesErrorEnvelope(t *testing.T) {
	event := parseWSUpstreamEvent([]byte(`{"type":"error","status":429,"error":{"code":"rate_limit_exceeded","type":"requests","message":"Too many requests"}}`))
	if event.Type != "error" || event.Error == nil {
		t.Fatalf("expected top-level error to be parsed, got %#v", event)
	}
	if event.Error.Status != http.StatusTooManyRequests || event.Error.Code != "rate_limit_exceeded" || event.Error.Type != "requests" || event.Error.Message != "Too many requests" {
		t.Fatalf("unexpected parsed error: %#v", event.Error)
	}
	publicErr, ok := classifyWSPublicError(event.Error, event.Error.Status)
	if !ok || publicErr.Status != http.StatusTooManyRequests || publicErr.Code != "upstream_rate_limited" {
		t.Fatalf("expected rate limit public error, got %#v ok=%t", publicErr, ok)
	}
}

func TestParseWSUpstreamEventParsesResponseErrorEnvelope(t *testing.T) {
	event := parseWSUpstreamEvent([]byte(`{"type":"response.failed","response":{"id":"resp_failed","model":"gpt-4o","status":"failed","error":{"code":"context_length_exceeded","type":"invalid_request_error","message":"maximum context length exceeded"}}}`))
	if event.Type != "response.failed" || event.ResponseID != "resp_failed" {
		t.Fatalf("expected the event type and response id to be captured, got %#v", event)
	}
	if event.Error == nil || event.Error.Code != "context_length_exceeded" || event.Error.Status != http.StatusBadGateway {
		t.Fatalf("unexpected response error: %#v", event.Error)
	}
	publicErr, ok := classifyWSPublicError(event.Error, 0)
	if !ok || publicErr.Status != http.StatusBadRequest || publicErr.Code != "context_length_exceeded" {
		t.Fatalf("expected context limit public error, got %#v ok=%t", publicErr, ok)
	}
}

// 正常事件里 error 是 null，或者根本不是 JSON：都不算错误，照样转发
func TestParseWSUpstreamEventWithoutError(t *testing.T) {
	event := parseWSUpstreamEvent([]byte(`{"type":"response.completed","error":null,"response":{"id":"resp_1","status":"completed","error":null}}`))
	if event.Type != "response.completed" || event.ResponseID != "resp_1" || event.Error != nil {
		t.Fatalf("unexpected event: %#v", event)
	}
	if event := parseWSUpstreamEvent([]byte(`not json`)); event.Type != "" || event.Error != nil {
		t.Fatalf("expected an unparsable frame to carry nothing, got %#v", event)
	}
}

func TestNormalizeWSUpstreamErrorCode(t *testing.T) {
	var payload struct {
		Code any `json:"code"`
	}
	if err := json.Unmarshal([]byte(`{"code":429}`), &payload); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}
	if got := normalizeWSUpstreamErrorCode(payload.Code); got != "429" {
		t.Fatalf("expected numeric code to normalize to 429, got %q", got)
	}
}
