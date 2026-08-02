package sitesync

import (
	"net/http"
	"net/http/httptest"
	"testing"

	dbpkg "github.com/bestruirui/octopus/internal/db"
	"github.com/bestruirui/octopus/internal/model"
	"github.com/bestruirui/octopus/internal/op"
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

func TestSyncAccountBindsFirstPricingRefreshToProjectedCandidate(t *testing.T) {
	ctx := setupProjectTestDB(t)
	// 目录默认改为手动建组后，未选中的模型不会产生路由候选；
	// 本用例验证的是「有候选时价格必须绑定」，所以显式沿用自动建组。
	if err := op.SettingRefreshCache(ctx); err != nil {
		t.Fatalf("refresh setting cache: %v", err)
	}
	if err := op.SettingSetString(
		model.SettingKeyCatalogGroupProvisioning,
		string(model.CatalogGroupProvisioningAuto),
	); err != nil {
		t.Fatalf("enable auto catalog provisioning: %v", err)
	}
	const (
		accessToken = "sync-pricing-access-token"
		modelName   = "gpt-sync-pricing"
	)
	pricingCalls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer "+accessToken {
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"error":{"message":"unauthorized"}}`))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/v1/models":
			_, _ = w.Write([]byte(`{"data":[{"id":"` + modelName + `"}]}`))
		case "/api/pricing":
			pricingCalls++
			_, _ = w.Write([]byte(`{"data":[{
				"model_name":"` + modelName + `",
				"enable_groups":["default"],
				"input_price":1.25,
				"output_price":3.5,
				"currency":"USD"
			}]}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	site := model.Site{
		Name:     "Sync Pricing Site",
		Platform: model.SitePlatformAPI,
		BaseURL:  server.URL,
		Enabled:  true,
	}
	if err := op.SiteCreate(&site, ctx); err != nil {
		t.Fatalf("create site: %v", err)
	}
	account := model.SiteAccount{
		SiteID:         site.ID,
		Name:           "Sync Pricing Account",
		CredentialType: model.SiteCredentialTypeAccessToken,
		AccessToken:    accessToken,
		Enabled:        true,
		AutoSync:       true,
	}
	if err := op.SiteAccountCreate(&account, ctx); err != nil {
		t.Fatalf("create account: %v", err)
	}

	result, err := SyncAccount(ctx, account.ID)
	if err != nil {
		t.Fatalf("SyncAccount: %v", err)
	}
	if result.ModelCount != 1 || pricingCalls != 1 {
		t.Fatalf("unexpected sync/pricing result: result=%+v pricing_calls=%d", result, pricingCalls)
	}
	var quote model.SiteModelPriceQuote
	if err := dbpkg.GetDB().WithContext(ctx).
		Where("site_account_id = ? AND model_name = ?", account.ID, modelName).
		First(&quote).Error; err != nil {
		t.Fatalf("load projected quote: %v", err)
	}
	if quote.RouteCandidateID == nil || *quote.RouteCandidateID <= 0 {
		t.Fatalf("first pricing refresh was not candidate-bound: %+v", quote)
	}
	var candidate model.RouteCandidate
	if err := dbpkg.GetDB().WithContext(ctx).First(&candidate, *quote.RouteCandidateID).Error; err != nil {
		t.Fatalf("load bound candidate: %v", err)
	}
	if candidate.SiteAccountID == nil || *candidate.SiteAccountID != account.ID ||
		candidate.UpstreamModelName != modelName {
		t.Fatalf("quote bound to wrong candidate: quote=%+v candidate=%+v", quote, candidate)
	}
	var unboundCount int64
	if err := dbpkg.GetDB().WithContext(ctx).Model(&model.SiteModelPriceQuote{}).
		Where("site_account_id = ? AND model_name = ? AND route_candidate_id IS NULL", account.ID, modelName).
		Count(&unboundCount).Error; err != nil {
		t.Fatalf("count unbound quotes: %v", err)
	}
	if unboundCount != 0 {
		t.Fatalf("first sync left %d unbound pricing rows", unboundCount)
	}
}

func TestSyncAccountReturnsPartialResultWhenCatalogProjectionFails(t *testing.T) {
	ctx := setupProjectTestDB(t)
	const (
		accessToken = "sync-catalog-failure-token"
		modelName   = "gpt-sync-catalog-failure"
	)
	pricingCalls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer "+accessToken {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/v1/models":
			_, _ = w.Write([]byte(`{"data":[{"id":"` + modelName + `"}]}`))
		case "/api/pricing":
			pricingCalls++
			_, _ = w.Write([]byte(`{"data":[{"model_name":"` + modelName + `","input_price":1}]}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	site := model.Site{
		Name: "Catalog Failure Site", Platform: model.SitePlatformAPI,
		BaseURL: server.URL, Enabled: true,
	}
	if err := op.SiteCreate(&site, ctx); err != nil {
		t.Fatalf("create site: %v", err)
	}
	account := model.SiteAccount{
		SiteID: site.ID, Name: "Catalog Failure Account",
		CredentialType: model.SiteCredentialTypeAccessToken,
		AccessToken:    accessToken, Enabled: true, AutoSync: true,
	}
	if err := op.SiteAccountCreate(&account, ctx); err != nil {
		t.Fatalf("create account: %v", err)
	}
	if err := dbpkg.GetDB().WithContext(ctx).
		Migrator().DropTable(&model.CanonicalModel{}); err != nil {
		t.Fatalf("drop canonical model table: %v", err)
	}

	result, err := SyncAccount(ctx, account.ID)
	if err == nil {
		t.Fatal("catalog projection failure was not reported")
	}
	if result == nil || result.Status != model.SiteExecutionStatusPartial ||
		result.ChannelCount != 1 || result.ModelCount != 1 {
		t.Fatalf("persisted projection was hidden by catalog failure: %+v", result)
	}
	if pricingCalls != 0 {
		t.Fatalf("pricing refresh ran without candidate catalog: %d calls", pricingCalls)
	}
	var bindingCount int64
	if countErr := dbpkg.GetDB().WithContext(ctx).Model(&model.SiteChannelBinding{}).
		Where("site_account_id = ?", account.ID).
		Count(&bindingCount).Error; countErr != nil {
		t.Fatalf("count persisted bindings: %v", countErr)
	}
	if bindingCount != 1 {
		t.Fatalf("catalog failure rolled back or hid projected binding: %d", bindingCount)
	}
	var reloaded model.SiteAccount
	if reloadErr := dbpkg.GetDB().WithContext(ctx).First(&reloaded, account.ID).Error; reloadErr != nil {
		t.Fatalf("reload account: %v", reloadErr)
	}
	if reloaded.LastSyncStatus != model.SiteExecutionStatusPartial {
		t.Fatalf("account sync state did not record partial projection: %+v", reloaded)
	}
}
