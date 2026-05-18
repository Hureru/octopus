package relay

import "strings"

func isWSStreamTerminalEvent(eventType string) bool {
	switch strings.TrimSpace(eventType) {
	case "response.completed",
		"response.failed",
		"response.incomplete",
		"response.cancelled",
		"response.canceled",
		"response.error",
		"response.done":
		return true
	default:
		return false
	}
}

func isWSStreamErrorEvent(eventType string) bool {
	switch strings.TrimSpace(eventType) {
	case "error", "response.failed", "response.error":
		return true
	default:
		return false
	}
}
