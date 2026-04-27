package compat

import (
	"strings"
	"time"

	"github.com/bestruirui/octopus/internal/utils/cache"
)

const geminiThoughtSignatureTTL = 24 * time.Hour

type geminiThoughtSignatureEntry struct {
	signature string
	expiresAt time.Time
}

var geminiThoughtSignatureCache = cache.New[string, geminiThoughtSignatureEntry](64)

// SaveGeminiThoughtSignature stores Gemini's opaque thoughtSignature without
// mutating the public tool_use ID that Anthropic clients keep in history.
func SaveGeminiThoughtSignature(toolCallID, toolName, signature string) {
	toolCallID = strings.TrimSpace(toolCallID)
	signature = strings.TrimSpace(signature)
	if toolCallID == "" || signature == "" {
		return
	}

	entry := geminiThoughtSignatureEntry{
		signature: signature,
		expiresAt: time.Now().Add(geminiThoughtSignatureTTL),
	}
	geminiThoughtSignatureCache.Set(geminiThoughtSignatureKey(toolCallID, toolName), entry)
	geminiThoughtSignatureCache.Set(geminiThoughtSignatureKey(toolCallID, ""), entry)
}

// RestoreGeminiThoughtSignature returns a cached Gemini thoughtSignature for
// a tool_use ID previously sent to an Anthropic-compatible client.
func RestoreGeminiThoughtSignature(toolCallID, toolName string) string {
	toolCallID = strings.TrimSpace(toolCallID)
	if toolCallID == "" {
		return ""
	}
	for _, key := range []string{
		geminiThoughtSignatureKey(toolCallID, toolName),
		geminiThoughtSignatureKey(toolCallID, ""),
	} {
		entry, ok := geminiThoughtSignatureCache.Get(key)
		if !ok {
			continue
		}
		if time.Now().After(entry.expiresAt) {
			geminiThoughtSignatureCache.Del(key)
			continue
		}
		return entry.signature
	}
	return ""
}

func geminiThoughtSignatureKey(toolCallID, toolName string) string {
	return strings.TrimSpace(toolCallID) + "\x00" + strings.TrimSpace(toolName)
}
