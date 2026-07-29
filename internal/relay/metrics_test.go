package relay

import (
	"math"
	"testing"

	"github.com/bestruirui/octopus/internal/model"
	transformerModel "github.com/bestruirui/octopus/internal/transformer/model"
)

// usage 完全缺失时，应使用 TransportInputTokens 兜底填充 input，output 保持 0。
func TestSetInternalResponseFallbackWhenUsageMissing(t *testing.T) {
	m := &RelayMetrics{TransportInputTokens: intPtr(123)}
	m.SetInternalResponse(&transformerModel.InternalLLMResponse{}, "test-model")

	if m.Stats.InputToken != 123 {
		t.Fatalf("input token: got %d want 123 (fallback)", m.Stats.InputToken)
	}
	if m.BillInputTokens == nil || *m.BillInputTokens != 123 {
		t.Fatalf("bill input tokens: got %v want 123", m.BillInputTokens)
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

	if m.Stats.InputToken != 0 {
		t.Fatalf("input token: got %d want 0 (cache-only is reported input)", m.Stats.InputToken)
	}
}

func TestRelayMetricsKeepsUnconvertibleSiteCreditOutOfUSDCost(t *testing.T) {
	m := &RelayMetrics{}
	m.SetEffectivePrice(model.EffectivePrice{
		QuoteID:           12,
		Source:            model.PriceQuoteSourceSiteExact,
		Unit:              model.PriceUnitSiteCredit,
		Currency:          "SITE_CREDIT",
		Input:             4,
		Output:            8,
		GroupMultiplier:   2,
		Convertible:       false,
		ExchangeRateToUSD: 0,
	})
	m.SetInternalResponse(&transformerModel.InternalLLMResponse{
		Usage: &transformerModel.Usage{
			PromptTokens:     1_000_000,
			CompletionTokens: 500_000,
		},
	}, "credit-model")

	if math.Abs(m.PriceOriginalCost-8) > 1e-9 {
		t.Fatalf("original site-credit cost = %f, want 8", m.PriceOriginalCost)
	}
	if m.Stats.InputCost != 0 || m.Stats.OutputCost != 0 {
		t.Fatalf("unconvertible credits leaked into USD cost: %+v", m.Stats)
	}
}

func TestRelayMetricsCalculatesPerRequestPriceOnce(t *testing.T) {
	m := &RelayMetrics{}
	m.SetEffectivePrice(model.EffectivePrice{
		QuoteID:           13,
		Source:            model.PriceQuoteSourceSiteExact,
		Unit:              model.PriceUnitPerRequest,
		Currency:          "CNY",
		PerRequest:        5,
		GroupMultiplier:   1,
		Convertible:       true,
		ExchangeRateToUSD: 0.2,
	})
	m.SetInternalResponse(&transformerModel.InternalLLMResponse{
		Usage: &transformerModel.Usage{
			PromptTokens:     2_000_000,
			CompletionTokens: 3_000_000,
		},
	}, "request-model")

	if m.PriceOriginalCost != 5 {
		t.Fatalf("per-request original cost = %f, want 5", m.PriceOriginalCost)
	}
	if math.Abs(m.Stats.InputCost-1) > 1e-9 || m.Stats.OutputCost != 0 {
		t.Fatalf("per-request converted cost mismatch: %+v", m.Stats)
	}
}

func TestFinalChannelRetainsClientCanceledAttempt(t *testing.T) {
	channelID, channelName := finalChannel([]model.ChannelAttempt{
		{
			ChannelID:   17,
			ChannelName: "images-channel",
			Status:      model.AttemptCanceled,
		},
	})
	if channelID != 17 || channelName != "images-channel" {
		t.Fatalf("finalChannel canceled attempt = (%d, %q)", channelID, channelName)
	}
}
