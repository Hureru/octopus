package relay

import (
	"encoding/json"
	"net/http"
	"testing"
	"time"

	dbmodel "github.com/bestruirui/octopus/internal/model"
	transformerModel "github.com/bestruirui/octopus/internal/transformer/model"
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

// 只把协议里的模型名（response.model）换回客户端用的那个，其余的值不变：
// 调用方自己的 metadata 不改，超出 float64 精度的整数不丢精度。
func TestRewriteWSPassthroughDownstreamModel(t *testing.T) {
	ra := &relayAttempt{relayRequest: &relayRequest{
		requestModel:    "my-alias",
		internalRequest: &transformerModel.InternalLLMRequest{Model: "gpt-4o"},
	}}
	for name, tc := range map[string]struct{ in, want string }{
		"response.model": {
			`{"type":"response.created","response":{"id":"resp_1","model":"gpt-4o","seed":9007199254740993}}`,
			`{"response":{"id":"resp_1","model":"my-alias","seed":9007199254740993},"type":"response.created"}`,
		},
		"response is null": {
			`{"type":"error","response":null,"model":"gpt-4o"}`,
			`{"type":"error","response":null,"model":"gpt-4o"}`,
		},
		"caller metadata is kept": {
			`{"type":"response.completed","response":{"model":"gpt-4o","metadata":{"model":"gpt-4o"}}}`,
			`{"response":{"metadata":{"model":"gpt-4o"},"model":"my-alias"},"type":"response.completed"}`,
		},
		"another model is kept": {
			`{"type":"response.created","response":{"model":"o3"}}`,
			`{"type":"response.created","response":{"model":"o3"}}`,
		},
		"no model": {
			`{"type":"response.output_text.delta","delta":"gpt-4o"}`,
			`{"type":"response.output_text.delta","delta":"gpt-4o"}`,
		},
		"not json": {`not json`, `not json`},
	} {
		if got := string(ra.rewriteWSPassthroughDownstreamModel([]byte(tc.in))); got != tc.want {
			t.Errorf("%s: got %s, want %s", name, got, tc.want)
		}
	}
}

// 渠道的参数覆盖可以改普通字段，但改不掉 WS 协议要求的那几个。
func TestBuildUpstreamWSPayloadKeepsTheWSEnvelope(t *testing.T) {
	override := `{"stream":false,"background":true,"type":"other","service_tier":"flex"}`
	ra := &relayAttempt{
		relayRequest: &relayRequest{internalRequest: &transformerModel.InternalLLMRequest{Model: "gpt-4o"}},
		channel:      &dbmodel.Channel{ParamOverride: &override},
	}
	payload, err := ra.buildUpstreamWSPayload([]byte(`{"model":"alias","input":"hi"}`))
	if err != nil {
		t.Fatalf("buildUpstreamWSPayload failed: %v", err)
	}
	want := `{"input":"hi","model":"gpt-4o","service_tier":"flex","stream":true,"type":"response.create"}`
	if string(payload) != want {
		t.Fatalf("payload = %s, want %s", payload, want)
	}
}

// “上游不支持 WS”的 30 分钟暂停，不能被同时发生的普通失败缩短，也不能被别的连接上的成功清掉。
func TestWSUpstreamHealthKeepsTheLongerPause(t *testing.T) {
	resetWSUpstreamHealth()
	t.Cleanup(resetWSUpstreamHealth)
	remaining := func() time.Duration {
		wsUpstreamHealth.Lock()
		defer wsUpstreamHealth.Unlock()
		return time.Until(wsUpstreamHealth.channels[1].skipUntil)
	}

	recordWSUpstreamFailure(1, true)
	recordWSUpstreamFailure(1, false)
	if got := remaining(); got < wsUnsupportedRecheck-time.Minute {
		t.Fatalf("an ordinary failure shortened the pause to %s", got)
	}
	recordWSUpstreamSuccess(1)
	if got := remaining(); got < wsUnsupportedRecheck-time.Minute {
		t.Fatalf("a success on another connection shortened the pause to %s", got)
	}

	// 没有暂停时，成功照常把失败次数清掉
	recordWSUpstreamSuccess(2)
	if !wsUpstreamAvailable(2) {
		t.Fatalf("a channel without a pause must stay available")
	}
}

// 上游用错误码说“找不到上一个回复”时，不管文字怎么写，客户端都要拿到能让它重发完整请求的那个码。
func TestPreviousResponseNotFoundIsRecognizedByCode(t *testing.T) {
	body := `{"error":{"message":"Unknown response ID","type":"invalid_request_error","code":"previous_response_not_found"}}`
	if got := normalizeUpstreamStatusCode(http.StatusBadRequest, body); got != http.StatusConflict {
		t.Fatalf("HTTP upstream: status = %d, want 409", got)
	}
	event := parseWSUpstreamEvent([]byte(`{"type":"error","status":400,"error":{"code":"previous_response_not_found","type":"invalid_request_error","message":"Unknown response ID"}}`))
	publicErr, ok := classifyWSPublicError(event.Error, event.Error.Status)
	if !ok || publicErr.Code != "previous_response_not_found" || !publicErr.ResetConversation {
		t.Fatalf("WS upstream: public error = %#v ok=%t", publicErr, ok)
	}
}
