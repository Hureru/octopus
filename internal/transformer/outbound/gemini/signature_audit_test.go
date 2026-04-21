package gemini

import (
	"testing"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"go.uber.org/zap/zaptest/observer"

	"github.com/bestruirui/octopus/internal/transformer/model"
	"github.com/bestruirui/octopus/internal/utils/log"
)

func installObserver(t *testing.T, level zapcore.Level) (*observer.ObservedLogs, func()) {
	t.Helper()
	core, recorded := observer.New(level)
	prev := log.Logger
	log.Logger = zap.New(core).Sugar()
	return recorded, func() { log.Logger = prev }
}

func TestLogGeminiSignatureAuditExtract(t *testing.T) {
	recorded, restore := installObserver(t, zapcore.DebugLevel)
	defer restore()

	blocks := []model.ReasoningBlock{
		{Kind: model.ReasoningBlockKindThinking, Text: "reasoning", Signature: "sigA", Provider: "gemini"},
		{Kind: model.ReasoningBlockKindSignature, Signature: "sigB", Provider: "gemini"},
	}

	logGeminiSignatureAudit("extract", blocks)

	entries := recorded.All()
	if len(entries) != 1 {
		t.Fatalf("expected 1 log entry, got %d", len(entries))
	}
	entry := entries[0]
	if entry.Message != "transformer.reasoning.signature.passthrough" {
		t.Errorf("message mismatch: %q", entry.Message)
	}
	fields := entry.ContextMap()
	if fields["provider"] != "gemini" {
		t.Errorf("provider mismatch: %v", fields["provider"])
	}
	if fields["direction"] != "extract" {
		t.Errorf("direction mismatch: %v", fields["direction"])
	}
	if fields["thinking_count"] != int64(1) {
		t.Errorf("thinking_count mismatch: %v", fields["thinking_count"])
	}
	if fields["signature_count"] != int64(2) {
		t.Errorf("signature_count mismatch: %v", fields["signature_count"])
	}
}

func TestLogGeminiSignatureAuditNoopOnEmpty(t *testing.T) {
	recorded, restore := installObserver(t, zapcore.DebugLevel)
	defer restore()

	logGeminiSignatureAudit("extract", nil)
	logGeminiSignatureAudit("extract", []model.ReasoningBlock{
		{Kind: model.ReasoningBlockKindSignature, Signature: ""},
	})

	if count := len(recorded.All()); count != 0 {
		t.Fatalf("expected zero log entries, got %d", count)
	}
}
