package sitesync

import (
	"encoding/json"
	"math"
	"testing"

	"github.com/bestruirui/octopus/internal/model"
)

func TestNormalizeCommonPricingPayload(t *testing.T) {
	raw := `{
		"data": [
			{
				"model_name": "gpt-4.1",
				"quota_type": 0,
				"model_ratio": 1.25,
				"completion_ratio": 4,
				"cache_ratio": 0.5,
				"cache_creation_ratio": 1.25,
				"enable_groups": ["default", "vip"]
			},
			{
				"model_name": "flat-call-model",
				"quota_type": 1,
				"model_price": 0.02,
				"enable_groups": ["default"]
			}
		],
		"group_ratio": {"default": 1.0, "vip": 1.5}
	}`

	var payload map[string]any
	if err := json.Unmarshal([]byte(raw), &payload); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	catalog := normalizeCommonPricingPayload(payload)
	if catalog == nil {
		t.Fatal("expected catalog, got nil")
	}
	if len(catalog.models) != 2 {
		t.Fatalf("expected 2 models, got %d", len(catalog.models))
	}
	if got := catalog.groupRatio["vip"]; !floatEq(got, 1.5) {
		t.Fatalf("vip ratio = %v, want 1.5", got)
	}
	if got := catalog.groupRatio["default"]; !floatEq(got, 1.0) {
		t.Fatalf("default ratio = %v, want 1.0", got)
	}

	prices := buildSitePrices(42, catalog, nil)
	got := make(map[string]model.SitePrice, len(prices))
	for _, p := range prices {
		got[p.GroupKey+"|"+p.ModelName] = p
	}

	vip := got["vip|gpt-4.1"]
	if !floatEq(vip.InputPrice, 1.25*1.5*sitePricingNewAPIBaseUSDPerMillion) {
		t.Fatalf("vip input price = %v, want %v", vip.InputPrice, 1.25*1.5*sitePricingNewAPIBaseUSDPerMillion)
	}
	if !floatEq(vip.OutputPrice, vip.InputPrice*4) {
		t.Fatalf("vip output price = %v, want %v", vip.OutputPrice, vip.InputPrice*4)
	}
	if !floatEq(vip.CacheReadPrice, vip.InputPrice*0.5) {
		t.Fatalf("vip cache read price = %v, want %v", vip.CacheReadPrice, vip.InputPrice*0.5)
	}
	if !floatEq(vip.CacheWritePrice, vip.InputPrice*1.25) {
		t.Fatalf("vip cache write price = %v, want %v", vip.CacheWritePrice, vip.InputPrice*1.25)
	}
	if vip.SiteAccountID != 42 {
		t.Fatalf("account id not plumbed through: %d", vip.SiteAccountID)
	}

	def := got["default|gpt-4.1"]
	if !floatEq(def.InputPrice, 1.25*1.0*sitePricingNewAPIBaseUSDPerMillion) {
		t.Fatalf("default input price = %v, want %v", def.InputPrice, 1.25*1.0*sitePricingNewAPIBaseUSDPerMillion)
	}

	flat := got["default|flat-call-model"]
	if flat.QuotaType != 1 {
		t.Fatalf("flat model quota type = %d, want 1", flat.QuotaType)
	}
	if !floatEq(flat.FlatPrice, 0.02) {
		t.Fatalf("flat price = %v, want 0.02", flat.FlatPrice)
	}
	if !floatEq(flat.InputPrice, 0) || !floatEq(flat.OutputPrice, 0) {
		t.Fatalf("flat-call model should not emit token prices, got input=%v output=%v", flat.InputPrice, flat.OutputPrice)
	}

	if _, ok := got["vip|flat-call-model"]; ok {
		t.Fatal("flat-call model should only appear in default group per enable_groups")
	}
}

func TestNormalizeOneHubPricingPayload(t *testing.T) {
	availableRaw := `{
		"data": {
			"gpt-4o": {
				"price": {"input": 5, "output": 15, "type": "tokens", "input_cache_read": 2.5},
				"groups": ["default", "pro"]
			},
			"text-flat": {
				"price": {"input": 0.01, "type": "times"},
				"groups": ["default"]
			}
		}
	}`
	groupRaw := `{
		"data": {
			"default": {"ratio": 1.0},
			"pro": {"ratio": 0.8}
		}
	}`

	var available, groupMap map[string]any
	if err := json.Unmarshal([]byte(availableRaw), &available); err != nil {
		t.Fatalf("available unmarshal: %v", err)
	}
	if err := json.Unmarshal([]byte(groupRaw), &groupMap); err != nil {
		t.Fatalf("group unmarshal: %v", err)
	}

	catalog := normalizeOneHubPricingPayload(available, groupMap)
	if catalog == nil {
		t.Fatal("expected catalog, got nil")
	}
	prices := buildSitePrices(7, catalog, nil)
	got := make(map[string]model.SitePrice, len(prices))
	for _, p := range prices {
		got[p.GroupKey+"|"+p.ModelName] = p
	}

	def := got["default|gpt-4o"]
	if !floatEq(def.InputPrice, 5) {
		t.Fatalf("one-hub default input price = %v, want 5", def.InputPrice)
	}
	if !floatEq(def.OutputPrice, 15) {
		t.Fatalf("one-hub default output price = %v, want 15", def.OutputPrice)
	}
	if !floatEq(def.CacheReadPrice, 2.5) {
		t.Fatalf("one-hub default cache read = %v, want 2.5", def.CacheReadPrice)
	}

	pro := got["pro|gpt-4o"]
	if !floatEq(pro.InputPrice, 5*0.8) {
		t.Fatalf("one-hub pro input price = %v, want %v", pro.InputPrice, 5*0.8)
	}
	if !floatEq(pro.OutputPrice, 15*0.8) {
		t.Fatalf("one-hub pro output price = %v, want %v", pro.OutputPrice, 15*0.8)
	}

	flat := got["default|text-flat"]
	if flat.QuotaType != 1 {
		t.Fatalf("text-flat quota type = %d, want 1 (times)", flat.QuotaType)
	}
	if !floatEq(flat.FlatPrice, 0.01) {
		t.Fatalf("text-flat flat price = %v, want 0.01", flat.FlatPrice)
	}
}

func TestBuildSitePricesFallsBackToSiteGroups(t *testing.T) {
	catalog := &pricingCatalog{
		models: []pricingModel{{
			modelName:       "gpt-foo",
			modelRatio:      1,
			completionRatio: 2,
		}},
		groupRatio: map[string]float64{model.SiteDefaultGroupKey: 1.0},
	}
	siteGroups := []model.SiteUserGroup{{GroupKey: "svip"}}
	prices := buildSitePrices(1, catalog, siteGroups)

	groups := make(map[string]struct{}, len(prices))
	for _, p := range prices {
		groups[p.GroupKey] = struct{}{}
	}
	if _, ok := groups["svip"]; !ok {
		t.Fatalf("expected svip group in output, got %v", groups)
	}
	if _, ok := groups[model.SiteDefaultGroupKey]; !ok {
		t.Fatalf("expected default group in output, got %v", groups)
	}
}

func floatEq(a, b float64) bool {
	return math.Abs(a-b) < 1e-9
}
