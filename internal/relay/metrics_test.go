package relay

import (
	"testing"

	"github.com/bestruirui/octopus/internal/model"
	"github.com/bestruirui/octopus/internal/op"
	transformerModel "github.com/bestruirui/octopus/internal/transformer/model"
)

// usage 完全缺失时，应使用 TransportInputTokens 兜底填充 input，output 保持 0。
func TestSetInternalResponseFallbackWhenUsageMissing(t *testing.T) {
	m := &RelayMetrics{TransportInputTokens: intPtr(123)}
	m.SetInternalResponse(&transformerModel.InternalLLMResponse{}, "test-model")

	if m.Stats.InputToken != 123 {
		t.Fatalf("input token: got %d want 123 (fallback)", m.Stats.InputToken)
	}
	if m.Stats.OutputToken != 0 {
		t.Fatalf("output token: got %d want 0", m.Stats.OutputToken)
	}
}

// usage 存在但输入侧全为 0（仅上报 output）时，input 兜底、output 保留。
func TestSetInternalResponseFallbackWhenInputZero(t *testing.T) {
	m := &RelayMetrics{TransportInputTokens: intPtr(50)}
	m.SetInternalResponse(&transformerModel.InternalLLMResponse{
		Usage: &transformerModel.Usage{PromptTokens: 0, CompletionTokens: 30},
	}, "test-model")

	if m.Stats.InputToken != 50 {
		t.Fatalf("input token: got %d want 50 (fallback)", m.Stats.InputToken)
	}
	if m.Stats.OutputToken != 30 {
		t.Fatalf("output token: got %d want 30 (preserved)", m.Stats.OutputToken)
	}
}

// 上游正常上报 input 时不触发兜底（保留真实值，而非估算值）。
func TestSetInternalResponseNoFallbackWhenInputReported(t *testing.T) {
	m := &RelayMetrics{TransportInputTokens: intPtr(999)}
	m.SetInternalResponse(&transformerModel.InternalLLMResponse{
		Usage: &transformerModel.Usage{PromptTokens: 12, CompletionTokens: 7},
	}, "test-model")

	if m.Stats.InputToken != 12 {
		t.Fatalf("input token: got %d want 12 (reported, not fallback)", m.Stats.InputToken)
	}
	if m.Stats.OutputToken != 7 {
		t.Fatalf("output token: got %d want 7", m.Stats.OutputToken)
	}
}

// 仅缓存命中（input_tokens=0 但 cache_read>0）属于已上报输入，不应被估算覆盖。
func TestSetInternalResponseNoFallbackWhenCacheOnly(t *testing.T) {
	m := &RelayMetrics{TransportInputTokens: intPtr(999)}
	m.SetInternalResponse(&transformerModel.InternalLLMResponse{
		Usage: &transformerModel.Usage{PromptTokens: 0, CacheReadInputTokens: 40, CompletionTokens: 5},
	}, "test-model")

	if m.Stats.InputToken != 40 || m.Stats.CacheReadToken != 40 {
		t.Fatalf("got input=%d cache_read=%d, want 40/40 (cache-only is reported input)", m.Stats.InputToken, m.Stats.CacheReadToken)
	}
}

func TestPublishProgressOnlyAfterAFailedAttempt(t *testing.T) {
	sub := op.RelayLogSubscribe()
	defer op.RelayLogUnsubscribe(sub)
	m := NewRelayMetrics(0, "gpt-4o-mini", nil, nil)
	defer m.DropLive()

	running := model.ChannelAttempt{ChannelID: 2, ChannelName: "b", ModelName: "m2", Status: model.AttemptRunning}
	skipped := model.ChannelAttempt{ChannelID: 1, ChannelName: "a", Status: model.AttemptSkipped}
	failed := model.ChannelAttempt{ChannelID: 1, ChannelName: "a", ModelName: "m1", Status: model.AttemptFailed}

	// 没失败过的请求不露面：一次成功的请求不应该先闪一张进行中的卡片
	m.PublishProgress([]model.ChannelAttempt{skipped, running})
	if m.logID != 0 || len(sub) != 0 {
		t.Fatalf("request without a failed attempt must not be published (logID=%d, events=%d)", m.logID, len(sub))
	}

	m.PublishProgress([]model.ChannelAttempt{failed})
	first := <-sub
	if !first.Pending || first.ID == 0 || first.ID != m.logID || first.RequestModelName != "gpt-4o-mini" {
		t.Fatalf("unexpected first snapshot: %+v", first)
	}

	m.PublishProgress([]model.ChannelAttempt{failed, running})
	second := <-sub
	if second.ID != first.ID {
		t.Fatalf("snapshots of one request must share an id: %d vs %d", first.ID, second.ID)
	}
	if second.ChannelName != "b" || second.ActualModelName != "m2" || second.TotalAttempts != 2 {
		t.Fatalf("snapshot should describe the attempt in flight, got %+v", second)
	}

	m.DropLive()
	for _, entry := range op.RelayLogLiveSnapshot() {
		if entry.ID == first.ID {
			t.Fatalf("DropLive should remove the live entry")
		}
	}
}

func TestReasoningEffort(t *testing.T) {
	cases := []struct {
		name     string
		internal string
		raw      string
		want     string
	}{
		{"normalized field wins", "high", `{"reasoning":{"effort":"xhigh"}}`, "high"},
		// Responses 入站白名单会清掉 xhigh，但透传时它确实发给了上游。
		{"responses value dropped by the inbound whitelist", "", `{"model":"gpt-5","reasoning":{"effort":"xhigh"}}`, "xhigh"},
		{"chat body", "", `{"reasoning_effort":"none"}`, "none"},
		{"anthropic effort without thinking", "", `{"output_config":{"effort":"max"}}`, "max"},
		{"wrong field type is ignored", "", `{"reasoning":"on","reasoning_effort":"low"}`, "low"},
		{"not requested", "", `{"model":"gpt-5"}`, ""},
		{"body is not json", "", `not json`, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := NewRelayMetrics(0, "m", []byte(tc.raw), &transformerModel.InternalLLMRequest{ReasoningEffort: tc.internal})
			if got := m.reasoningEffort(); got != tc.want {
				t.Fatalf("reasoningEffort() = %q, want %q", got, tc.want)
			}
		})
	}
	if got := (&RelayMetrics{}).reasoningEffort(); got != "" {
		t.Fatalf("no request at all: got %q, want empty", got)
	}
}
