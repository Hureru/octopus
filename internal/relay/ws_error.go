package relay

import (
	"errors"
	"io"
	"net"
	"net/http"
	"strings"

	"github.com/bestruirui/octopus/internal/relay/stream"
	transformerModel "github.com/bestruirui/octopus/internal/transformer/model"
	"github.com/bestruirui/octopus/internal/utils/log"
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

func isUpstreamWSConnectionBroken(err error) bool {
	if err == nil {
		return false
	}
	// Prefer type-based checks for reliable detection across library versions.
	if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
		return true
	}
	var netErr *net.OpError
	if errors.As(err, &netErr) {
		return true
	}
	// Fallback to string matching for errors from libraries that don't export typed errors
	// (e.g., nhooyr/websocket close frames, gorilla/websocket internal messages).
	message := relayErrorMessage(err)
	return strings.Contains(message, "broken pipe") ||
		strings.Contains(message, "failed to read frame header") ||
		strings.Contains(message, "use of closed network connection") ||
		strings.Contains(message, "websocket: close sent") ||
		strings.Contains(message, "failed to get reader") ||
		strings.Contains(message, "connection reset by peer")
}

func shouldReconnectUpstreamWSBeforeReplay(err error) bool {
	// Empty stream before first event should trigger reconnect
	if errors.Is(err, stream.ErrEmptyUpstreamStream) {
		log.Debugf("ws continuation error marked reconnectable before replay: %v", err)
		return true
	}

	message := relayErrorMessage(err)
	if message == "" {
		return false
	}
	shouldReconnect := isUpstreamWSConnectionBroken(err) ||
		strings.Contains(message, "ws stream ended before first event")
	if shouldReconnect {
		log.Debugf("ws continuation error marked reconnectable before replay: %v", err)
	}
	return shouldReconnect
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

func requiresUpstreamWSContinuation(req *transformerModel.InternalLLMRequest) bool {
	if req == nil {
		return false
	}
	if req.OpenAIPreviousResponseID() != "" {
		return true
	}
	if len(req.GetOpenAIResponsesOptions().Conversation) > 0 {
		return true
	}
	seenToolCalls := make(map[string]struct{})
	for _, msg := range req.Messages {
		if msg.Role == "assistant" {
			for _, toolCall := range msg.ToolCalls {
				if toolCallID := strings.TrimSpace(toolCall.ID); toolCallID != "" {
					seenToolCalls[toolCallID] = struct{}{}
				}
			}
		}
		if msg.Role != "tool" || msg.ToolCallID == nil {
			continue
		}
		if toolCallID := strings.TrimSpace(*msg.ToolCallID); toolCallID != "" {
			if _, exists := seenToolCalls[toolCallID]; exists {
				continue
			}
			return true
		}
	}
	return false
}
