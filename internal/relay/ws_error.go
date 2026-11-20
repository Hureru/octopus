package relay

import (
	"errors"
	"net/http"
	"strings"
)

type wsPublicError struct {
	Status            int
	Code              string
	Message           string
	ResetConversation bool
}

func classifyWSPublicError(err error, statusCode int) (wsPublicError, bool) {
	message := relayErrorMessage(err)
	var wsErr *wsUpstreamEventError
	if errors.As(err, &wsErr) && wsErr != nil {
		if wsErr.Status > 0 {
			statusCode = wsErr.Status
		}
		if wsErr.Code != "" {
			message += " " + strings.ToLower(wsErr.Code)
		}
		if wsErr.Type != "" {
			message += " " + strings.ToLower(wsErr.Type)
		}
	}
	switch {
	case needsConversationRestart(message):
		return wsPublicError{
			Status: http.StatusConflict,
			// Codex 等客户端认识这个码：收到后会不带 previous_response_id 重发完整请求。
			Code:              "previous_response_not_found",
			Message:           "上游连续会话已中断，请重新开启对话后再试",
			ResetConversation: true,
		}, true
	case isNoAvailableAccountError(message):
		return wsPublicError{
			Status:  http.StatusServiceUnavailable,
			Code:    "no_available_account",
			Message: "上游暂无可用账号，请稍后重试",
		}, true
	case isUpstreamRateLimitError(message):
		return wsPublicError{
			Status:  http.StatusTooManyRequests,
			Code:    "upstream_rate_limited",
			Message: "上游限流，请稍后重试",
		}, true
	case isUpstreamContextLimitError(message):
		return wsPublicError{
			Status:  http.StatusBadRequest,
			Code:    "context_length_exceeded",
			Message: "请求上下文超过上游限制，请缩短对话后重试",
		}, true
	case isUpstreamQuotaError(message):
		return wsPublicError{
			Status:  http.StatusServiceUnavailable,
			Code:    "upstream_quota_exceeded",
			Message: "上游额度不足或不可用，请稍后重试",
		}, true
	case isBlockedInvalidRequestError(message):
		return wsPublicError{
			Status:  http.StatusBadRequest,
			Code:    "invalid_request_blocked",
			Message: "请求被上游判定为无效，请检查请求内容后重试",
		}, true
	case statusCode >= 400 && statusCode < 500:
		return wsPublicError{
			Status:  statusCode,
			Code:    "upstream_invalid_request",
			Message: "上游拒绝了当前请求，请检查请求参数后重试",
		}, true
	default:
		return wsPublicError{}, false
	}
}

func normalizeUpstreamStatusCode(statusCode int, body string) int {
	message := strings.ToLower(body)
	switch {
	case needsConversationRestart(message):
		return http.StatusConflict
	case isBlockedInvalidRequestError(message):
		return http.StatusBadRequest
	default:
		return statusCode
	}
}

func relayErrorMessage(err error) string {
	if err == nil {
		return ""
	}
	return strings.ToLower(err.Error())
}

func needsConversationRestart(message string) bool {
	return strings.Contains(message, "please restart the conversation") ||
		strings.Contains(message, "continuation connection is unavailable") ||
		strings.Contains(message, "no tool call found for function call output with call_id") ||
		strings.Contains(message, "previous response") && strings.Contains(message, "not found")
}

func isNoAvailableAccountError(message string) bool {
	return strings.Contains(message, "no available account")
}

func isBlockedInvalidRequestError(message string) bool {
	return strings.Contains(message, "blocked_invalid_request")
}

func isUpstreamRateLimitError(message string) bool {
	return strings.Contains(message, "rate_limit_exceeded") ||
		strings.Contains(message, "rate limit") ||
		strings.Contains(message, "too many requests") ||
		strings.Contains(message, "status=429") ||
		strings.Contains(message, "status 429")
}

func isUpstreamContextLimitError(message string) bool {
	return strings.Contains(message, "context_length_exceeded") ||
		strings.Contains(message, "maximum context length") ||
		strings.Contains(message, "context window")
}

func isUpstreamQuotaError(message string) bool {
	return strings.Contains(message, "insufficient_quota") ||
		strings.Contains(message, "quota exceeded") ||
		strings.Contains(message, "billing") && strings.Contains(message, "hard limit")
}
