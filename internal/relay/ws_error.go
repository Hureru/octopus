package relay

import (
	"net/http"
	"strings"

	transformerModel "github.com/bestruirui/octopus/internal/transformer/model"
)

type wsPublicError struct {
	Status            int
	Code              string
	Message           string
	ResetConversation bool
}

func classifyWSPublicError(err error, statusCode int) (wsPublicError, bool) {
	message := relayErrorMessage(err)
	switch {
	case needsConversationRestart(message):
		return wsPublicError{
			Status:            http.StatusConflict,
			Code:              "conversation_restart_required",
			Message:           "上游连续会话已中断，请重新开启对话后再试",
			ResetConversation: true,
		}, true
	case isNoAvailableAccountError(message):
		return wsPublicError{
			Status:  http.StatusServiceUnavailable,
			Code:    "no_available_account",
			Message: "上游暂无可用账号，请稍后重试",
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

func requiresUpstreamWSContinuation(req *transformerModel.InternalLLMRequest) bool {
	if req == nil {
		return false
	}
	if req.PreviousResponseID != nil && strings.TrimSpace(*req.PreviousResponseID) != "" {
		return true
	}
	if len(req.Conversation) > 0 {
		return true
	}
	for _, msg := range req.Messages {
		if msg.Role != "tool" || msg.ToolCallID == nil {
			continue
		}
		if strings.TrimSpace(*msg.ToolCallID) != "" {
			return true
		}
	}
	return false
}
