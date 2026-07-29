package sitesync

import (
	"testing"

	"github.com/bestruirui/octopus/internal/model"
)

func TestParseSitePricingQuotesAppliesGroupMultiplierOnlyInResolver(t *testing.T) {
	payload := map[string]any{
		"group_ratio": map[string]any{"premium": 2.0},
		"data": []any{
			map[string]any{
				"model_name":    "gpt-price-test",
				"enable_groups": []any{"premium"},
				"token_price_usd_per_million": map[string]any{
					"input":       1.25,
					"output":      3.5,
					"cache_read":  0.2,
					"cache_write": 0.8,
				},
			},
		},
	}

	quotes := parseSitePricingQuotes(11, 22, payload)
	if len(quotes) != 1 {
		t.Fatalf("got %d quotes, want 1", len(quotes))
	}
	quote := quotes[0]
	if quote.Input != 1.25 || quote.Output != 3.5 || quote.CacheRead != 0.2 || quote.CacheWrite != 0.8 {
		t.Fatalf("parser multiplied base prices prematurely: %+v", quote)
	}
	if quote.GroupMultiplier != 2 {
		t.Fatalf("group multiplier = %f, want 2", quote.GroupMultiplier)
	}
	if quote.Source != model.PriceQuoteSourceSiteExact ||
		quote.Unit != model.PriceUnitPerMillionTokens ||
		quote.Currency != "USD" ||
		quote.ExchangeRateToUSD != 1 {
		t.Fatalf("unexpected quote metadata: %+v", quote)
	}
}

func TestParseSitePricingQuotesPreservesPerRequestAndSiteCreditUnits(t *testing.T) {
	payload := map[string]any{
		"currency":    "credits",
		"group_ratio": map[string]any{"default": 3.0},
		"data": []any{
			map[string]any{
				"model_name":  "per-request-model",
				"quota_type":  1.0,
				"model_price": 0.5,
			},
			map[string]any{
				"model_name":   "credit-model",
				"unit":         "site_credit",
				"input_price":  2.0,
				"output_price": 4.0,
			},
			map[string]any{
				"model_name":  "invalid-unit-model",
				"unit":        "per_second",
				"input_price": 1.0,
			},
		},
	}

	quotes := parseSitePricingQuotes(11, 22, payload)
	if len(quotes) != 3 {
		t.Fatalf("got %d quotes, want 3", len(quotes))
	}
	byModel := make(map[string]model.SiteModelPriceQuote, len(quotes))
	for _, quote := range quotes {
		byModel[quote.ModelName] = quote
	}

	perRequest := byModel["per-request-model"]
	if perRequest.Unit != model.PriceUnitPerRequest ||
		perRequest.Currency != "SITE_CREDIT" ||
		perRequest.PerRequest != 0.5 ||
		perRequest.GroupMultiplier != 3 {
		t.Fatalf("per-request quote mismatch: %+v", perRequest)
	}
	credit := byModel["credit-model"]
	if credit.Unit != model.PriceUnitSiteCredit ||
		credit.Currency != "SITE_CREDIT" ||
		credit.Input != 2 ||
		credit.Output != 4 {
		t.Fatalf("site-credit quote mismatch: %+v", credit)
	}
	if got := byModel["invalid-unit-model"].Unit; got != model.PriceUnit("per_second") {
		t.Fatalf("unknown unit %q was not preserved for rejection", got)
	}
}
