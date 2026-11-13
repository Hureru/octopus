package relay

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/bestruirui/octopus/internal/model"
	"github.com/bestruirui/octopus/internal/transformer/inbound"
	transformerModel "github.com/bestruirui/octopus/internal/transformer/model"
	"github.com/bestruirui/octopus/internal/transformer/outbound"
	"github.com/gin-gonic/gin"
)

// 回归 issue #100：上游 200 但响应体没有任何内容时必须算失败，且不能把空壳写给客户端。
func TestHandleResponseRejectsEmptyUpstreamResponse(t *testing.T) {
	for name, tc := range map[string]struct {
		body      string
		wantEmpty bool
	}{
		"empty object":  {body: `{}`, wantEmpty: true},
		"unknown shape": {body: `{"code":0,"msg":"ok"}`, wantEmpty: true},
		"no choices":    {body: `{"id":"c1","object":"chat.completion","model":"gpt-4o","choices":[]}`, wantEmpty: true},
		"normal":        {body: `{"id":"c1","object":"chat.completion","model":"gpt-4o","choices":[{"index":0,"message":{"role":"assistant","content":"hi"},"finish_reason":"stop"}]}`},
	} {
		t.Run(name, func(t *testing.T) {
			gin.SetMode(gin.TestMode)
			recorder := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(recorder)
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
			internalReq := &transformerModel.InternalLLMRequest{Model: "gpt-4o"}
			ra := &relayAttempt{
				relayRequest: &relayRequest{
					c:               c,
					inAdapter:       inbound.Get(inbound.InboundTypeOpenAIChat),
					internalRequest: internalReq,
					metrics:         NewRelayMetrics(1, internalReq.Model, nil, internalReq),
				},
				outAdapter: outbound.Get(outbound.OutboundTypeOpenAIChat),
				channel:    &model.Channel{Type: outbound.OutboundTypeOpenAIChat},
			}
			err := ra.handleResponse(context.Background(), &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(tc.body))})
			if tc.wantEmpty {
				if !errors.Is(err, errEmptyUpstreamResponse) {
					t.Fatalf("expected errEmptyUpstreamResponse, got: %v", err)
				}
				if recorder.Body.Len() != 0 {
					t.Fatalf("empty response must not be written to the client, got %q", recorder.Body.String())
				}
				return
			}
			if err != nil || !strings.Contains(recorder.Body.String(), `"content":"hi"`) {
				t.Fatalf("normal response should pass through, err=%v body=%q", err, recorder.Body.String())
			}
		})
	}
}
