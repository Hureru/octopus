package relay

import (
	"encoding/json"
	"net/http"
	"testing"
)

func TestObserveWSPassthroughEventParsesErrorEnvelope(t *testing.T) {
	stats := &wsPassthroughStats{}
	observeWSPassthroughEvent(stats, []byte(`{"type":"error","status":429,"error":{"code":"rate_limit_exceeded","type":"requests","message":"Too many requests"}}`))
	if stats.Error == nil {
		t.Fatalf("expected top-level error to be parsed")
	}
	if stats.Error.Status != http.StatusTooManyRequests || stats.Error.Code != "rate_limit_exceeded" || stats.Error.Type != "requests" || stats.Error.Message != "Too many requests" {
		t.Fatalf("unexpected parsed error: %#v", stats.Error)
	}
	publicErr, ok := classifyWSPublicError(stats.Error, stats.Error.Status)
	if !ok || publicErr.Status != http.StatusTooManyRequests || publicErr.Code != "upstream_rate_limited" {
		t.Fatalf("expected rate limit public error, got %#v ok=%t", publicErr, ok)
	}
}

func TestObserveWSPassthroughEventParsesResponseErrorEnvelope(t *testing.T) {
	stats := &wsPassthroughStats{}
	observeWSPassthroughEvent(stats, []byte(`{"type":"response.failed","response":{"id":"resp_failed","model":"gpt-4o","status":"failed","error":{"code":"context_length_exceeded","type":"invalid_request_error","message":"maximum context length exceeded"}}}`))
	if stats.ResponseID != "resp_failed" {
		t.Fatalf("expected response id to be captured, got %q", stats.ResponseID)
	}
	if stats.Error == nil || stats.Error.Code != "context_length_exceeded" || stats.Error.Status != http.StatusBadGateway {
		t.Fatalf("unexpected response error: %#v", stats.Error)
	}
	publicErr, ok := classifyWSPublicError(stats.Error, 0)
	if !ok || publicErr.Status != http.StatusBadRequest || publicErr.Code != "context_length_exceeded" {
		t.Fatalf("expected context limit public error, got %#v ok=%t", publicErr, ok)
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
