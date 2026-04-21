package anthropic

import (
	"testing"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"go.uber.org/zap/zaptest/observer"

	"github.com/bestruirui/octopus/internal/transformer/model"
	"github.com/bestruirui/octopus/internal/utils/log"
)

// installObserver swaps the package-level log.Logger with an observer-backed
// SugaredLogger so structured log events can be asserted. The returned
// restore function MUST be deferred.
func installObserver(t *testing.T, level zapcore.Level) (*observer.ObservedLogs, func()) {
	t.Helper()
	core, recorded := observer.New(level)
	prev := log.Logger
	log.Logger = zap.New(core).Sugar()
	return recorded, func() { log.Logger = prev }
}

func TestLogAnthropicSignatureAuditInject(t *testing.T) {
	recorded, restore := installObserver(t, zapcore.DebugLevel)
	defer restore()

	blocks := []model.ReasoningBlock{
		{Kind: model.ReasoningBlockKindThinking, Text: "hello", Signature: "sigA", Provider: "anthropic"},
		{Kind: model.ReasoningBlockKindRedacted, Data: "REDACTED", Provider: "anthropic"},
		{Kind: model.ReasoningBlockKindSignature, Signature: "sigB", Provider: "anthropic"},
	}

	logAnthropicSignatureAudit("inject", blocks)

	entries := recorded.All()
	if len(entries) != 1 {
		t.Fatalf("expected 1 log entry, got %d", len(entries))
	}
	entry := entries[0]
	if entry.Message != "transformer.reasoning.signature.passthrough" {
		t.Errorf("message mismatch: %q", entry.Message)
	}
	if entry.Level != zapcore.DebugLevel {
		t.Errorf("expected Debug level, got %v", entry.Level)
	}
	fields := entry.ContextMap()
	want := map[string]interface{}{
		"provider":        "anthropic",
		"direction":       "inject",
		"thinking_count":  int64(1),
		"redacted_count":  int64(1),
		"signature_count": int64(3),
	}
	for k, v := range want {
		if fields[k] != v {
			t.Errorf("field %s mismatch: got %v (%T), want %v", k, fields[k], fields[k], v)
		}
	}
}

func TestLogAnthropicSignatureAuditEmpty(t *testing.T) {
	recorded, restore := installObserver(t, zapcore.DebugLevel)
	defer restore()

	// Empty slice and blocks with no payload must not emit audit noise.
	logAnthropicSignatureAudit("extract", nil)
	logAnthropicSignatureAudit("extract", []model.ReasoningBlock{
		{Kind: model.ReasoningBlockKindSignature, Signature: ""},
	})

	if count := len(recorded.All()); count != 0 {
		t.Fatalf("expected zero log entries for empty/no-payload blocks, got %d", count)
	}
}
