package op

import (
	"context"
	"math"
	"testing"
	"time"

	dbpkg "github.com/bestruirui/octopus/internal/db"
	"github.com/bestruirui/octopus/internal/model"
)

func TestEffectivePricePrecedenceAndFallback(t *testing.T) {
	ctx := setupBackupTestDB(t)
	fixture := createPricingFixture(t, ctx, "precedence")
	now := time.Now()

	wide := model.SiteModelPriceQuote{
		SiteID: fixture.site.ID, GroupKey: model.SiteDefaultGroupKey,
		ModelName: fixture.candidate.UpstreamModelName,
		Source:    model.PriceQuoteSourceSiteWide, Unit: model.PriceUnitPerMillionTokens,
		Currency: "USD", Input: 1, Output: 2, GroupMultiplier: 1,
		ExchangeRateToUSD: 1, ObservedAt: now,
	}
	exact := model.SiteModelPriceQuote{
		SiteID: fixture.site.ID, SiteAccountID: &fixture.account.ID, GroupKey: fixture.candidate.SiteGroupKey,
		ModelName: fixture.candidate.UpstreamModelName,
		Source:    model.PriceQuoteSourceSiteExact, Unit: model.PriceUnitPerMillionTokens,
		Currency: "USD", Input: 2, Output: 4, GroupMultiplier: 2,
		ExchangeRateToUSD: 1, ObservedAt: now,
	}
	otherSite := model.Site{
		Name: "pricing-other-site", Platform: model.SitePlatformNewAPI,
		BaseURL: "https://pricing-other.example.com", Enabled: true,
	}
	mustCreatePricingRow(t, ctx, &otherSite)
	foreign := model.SiteModelPriceQuote{
		SiteID: otherSite.ID, GroupKey: model.SiteDefaultGroupKey,
		ModelName: fixture.candidate.UpstreamModelName,
		Source:    model.PriceQuoteSourceSiteWide, Unit: model.PriceUnitPerMillionTokens,
		Currency: "USD", Input: 0.01, Output: 0.02, GroupMultiplier: 1,
		ExchangeRateToUSD: 1, ObservedAt: now,
	}
	if err := SiteModelPriceQuotesUpsert(ctx, []model.SiteModelPriceQuote{wide, exact, foreign}); err != nil {
		t.Fatalf("SiteModelPriceQuotesUpsert failed: %v", err)
	}

	price, err := EffectivePriceForCandidate(ctx, fixture.candidate.ID, "")
	if err != nil {
		t.Fatalf("EffectivePriceForCandidate failed: %v", err)
	}
	if price.Source != model.PriceQuoteSourceSiteExact || price.Input != 4 || price.Output != 8 {
		t.Fatalf("exact quote was not selected with one multiplier application: %+v", price)
	}
	if price.RouteCandidateID != fixture.candidate.ID || price.QuoteID == 0 {
		t.Fatalf("effective price lost candidate or quote identity: %+v", price)
	}

	exact.Input = 3
	exact.Output = 5
	exact.ModelMultiplier = 0.3
	exact.ObservedAt = now.Add(time.Minute)
	if err := SiteModelPriceQuotesUpsert(ctx, []model.SiteModelPriceQuote{exact}); err != nil {
		t.Fatalf("upsert exact quote failed: %v", err)
	}
	var exactCount int64
	if err := dbpkg.GetDB().WithContext(ctx).Model(&model.SiteModelPriceQuote{}).
		Where("site_id = ? AND source = ?", fixture.site.ID, model.PriceQuoteSourceSiteExact).
		Count(&exactCount).Error; err != nil {
		t.Fatalf("count exact quotes failed: %v", err)
	}
	if exactCount != 1 {
		t.Fatalf("stable identity upsert created %d exact rows, want 1", exactCount)
	}
	var updatedExact model.SiteModelPriceQuote
	if err := dbpkg.GetDB().WithContext(ctx).
		Where("site_id = ? AND source = ?", fixture.site.ID, model.PriceQuoteSourceSiteExact).
		First(&updatedExact).Error; err != nil {
		t.Fatalf("load updated exact quote: %v", err)
	}
	if updatedExact.ModelMultiplier != exact.ModelMultiplier {
		t.Fatalf("model multiplier was not updated: got %f, want %f", updatedExact.ModelMultiplier, exact.ModelMultiplier)
	}

	manual, err := SiteModelPriceManualUpsert(ctx, model.SiteModelPriceQuote{
		RouteCandidateID: &fixture.candidate.ID,
		Unit:             model.PriceUnitPerMillionTokens,
		Currency:         "USD",
		Input:            9,
		Output:           11,
		GroupMultiplier:  1,
	})
	if err != nil {
		t.Fatalf("SiteModelPriceManualUpsert failed: %v", err)
	}
	price, err = EffectivePriceForCandidate(ctx, fixture.candidate.ID, "")
	if err != nil {
		t.Fatalf("resolve manual price failed: %v", err)
	}
	if price.Source != model.PriceQuoteSourceManualOverride || price.QuoteID != manual.ID || price.Input != 9 {
		t.Fatalf("manual quote did not win: %+v", price)
	}
	if err := SiteModelPriceManualDelete(ctx, manual.ID); err != nil {
		t.Fatalf("SiteModelPriceManualDelete failed: %v", err)
	}

	if err := dbpkg.GetDB().WithContext(ctx).Model(&model.SiteModelPriceQuote{}).
		Where("site_id = ? AND source = ?", fixture.site.ID, model.PriceQuoteSourceSiteExact).
		Update("observed_at", now.Add(-25*time.Hour)).Error; err != nil {
		t.Fatalf("age exact quote failed: %v", err)
	}
	price, err = EffectivePriceForCandidate(ctx, fixture.candidate.ID, "")
	if err != nil {
		t.Fatalf("resolve wide price failed: %v", err)
	}
	if price.Source != model.PriceQuoteSourceSiteWide || price.Input != 1 {
		t.Fatalf("fresh wide quote should outrank stale exact quote: %+v", price)
	}

	if err := dbpkg.GetDB().WithContext(ctx).Model(&model.SiteModelPriceQuote{}).
		Where("site_id = ? AND source = ?", fixture.site.ID, model.PriceQuoteSourceSiteWide).
		Update("observed_at", now.Add(-26*time.Hour)).Error; err != nil {
		t.Fatalf("age wide quote failed: %v", err)
	}
	price, err = EffectivePriceForCandidate(ctx, fixture.candidate.ID, "")
	if err != nil {
		t.Fatalf("resolve stale price failed: %v", err)
	}
	if price.Source != model.PriceQuoteSourceSiteStale || price.Input != 6 || !price.Stale {
		t.Fatalf("stale exact quote should win over stale wide quote: %+v", price)
	}

	if err := dbpkg.GetDB().WithContext(ctx).
		Where("site_id = ?", fixture.site.ID).
		Delete(&model.SiteModelPriceQuote{}).Error; err != nil {
		t.Fatalf("delete site quotes failed: %v", err)
	}
	if err := LLMCreate(model.LLMInfo{
		Name: fixture.candidate.UpstreamModelName,
		LLMPrice: model.LLMPrice{
			Input: 0.5, Output: 1,
		},
	}, ctx); err != nil {
		t.Fatalf("LLMCreate failed: %v", err)
	}
	price, err = EffectivePriceForCandidate(ctx, fixture.candidate.ID, "")
	if err != nil {
		t.Fatalf("resolve global price failed: %v", err)
	}
	if price.Source != model.PriceQuoteSourceGlobal || !price.Convertible || price.Input != 0.5 {
		t.Fatalf("global fallback mismatch: %+v", price)
	}
}

func TestChannelLLMListExposesPersistedGroupMultiplierForEveryModel(t *testing.T) {
	ctx := setupCatalogProvisionTest(t)
	multiplier := 0.0
	site := model.Site{
		Name: "group-multiplier-site", Platform: model.SitePlatformNewAPI,
		BaseURL: "https://group-multiplier.example.com", Enabled: true,
	}
	mustCreatePricingRow(t, ctx, &site)
	account := model.SiteAccount{
		SiteID: site.ID, Name: "group-multiplier-account",
		CredentialType: model.SiteCredentialTypeAccessToken, AccessToken: "token", Enabled: true,
	}
	mustCreatePricingRow(t, ctx, &account)
	group := model.SiteUserGroup{
		SiteAccountID: account.ID, GroupKey: "vip", Name: "VIP", Multiplier: &multiplier,
	}
	mustCreatePricingRow(t, ctx, &group)
	channel := model.Channel{
		Name: "group-multiplier-channel", Model: "priced-model,unpriced-model", Enabled: true,
	}
	if err := ChannelCreate(&channel, ctx); err != nil {
		t.Fatalf("create channel: %v", err)
	}
	binding := model.SiteChannelBinding{
		SiteID: site.ID, SiteAccountID: account.ID, SiteUserGroupID: &group.ID,
		GroupKey:  model.ComposeSiteChannelBindingKey("vip", model.SiteModelRouteTypeAnthropic, true),
		ChannelID: channel.ID,
	}
	mustCreatePricingRow(t, ctx, &binding)

	items, err := ChannelLLMList(ctx)
	if err != nil {
		t.Fatalf("ChannelLLMList: %v", err)
	}
	seen := 0
	for _, item := range items {
		if item.ChannelID != channel.ID {
			continue
		}
		seen++
		if item.IsReserve {
			t.Fatalf("non-relay channel was marked reserve: %+v", item)
		}
		if item.GroupMultiplier == nil || *item.GroupMultiplier != multiplier {
			t.Fatalf("group multiplier missing from model %q: %+v", item.Name, item)
		}
	}
	if seen != 2 {
		t.Fatalf("got %d channel models, want 2", seen)
	}
}

func TestSiteUserGroupMultipliersUpdatePersistsZero(t *testing.T) {
	ctx := setupCatalogProvisionTest(t)
	site := model.Site{
		Name: "zero-group-multiplier-site", Platform: model.SitePlatformNewAPI,
		BaseURL: "https://zero-group-multiplier.example.com", Enabled: true,
	}
	mustCreatePricingRow(t, ctx, &site)
	account := model.SiteAccount{
		SiteID: site.ID, Name: "zero-group-multiplier-account",
		CredentialType: model.SiteCredentialTypeAccessToken, AccessToken: "token", Enabled: true,
	}
	mustCreatePricingRow(t, ctx, &account)
	initial := 1.0
	group := model.SiteUserGroup{
		SiteAccountID: account.ID, GroupKey: "complimentary", Name: "Complimentary", Multiplier: &initial,
	}
	mustCreatePricingRow(t, ctx, &group)

	if err := SiteUserGroupMultipliersUpdate(ctx, account.ID, map[string]float64{"complimentary": 0}); err != nil {
		t.Fatalf("SiteUserGroupMultipliersUpdate: %v", err)
	}
	var reloaded model.SiteUserGroup
	if err := dbpkg.GetDB().WithContext(ctx).First(&reloaded, group.ID).Error; err != nil {
		t.Fatalf("reload group: %v", err)
	}
	if reloaded.Multiplier == nil || *reloaded.Multiplier != 0 {
		t.Fatalf("zero group multiplier was not persisted: %+v", reloaded)
	}
}

func TestChannelLLMListFallsBackToSiblingQuoteGroupMultiplier(t *testing.T) {
	ctx := setupCatalogProvisionTest(t)
	const expectedMultiplier = 0.0
	site := model.Site{
		Name: "quote-group-multiplier-site", Platform: model.SitePlatformNewAPI,
		BaseURL: "https://quote-group-multiplier.example.com", Enabled: true,
	}
	mustCreatePricingRow(t, ctx, &site)
	account := model.SiteAccount{
		SiteID: site.ID, Name: "quote-group-multiplier-account",
		CredentialType: model.SiteCredentialTypeAccessToken, AccessToken: "token", Enabled: true,
	}
	mustCreatePricingRow(t, ctx, &account)
	group := model.SiteUserGroup{SiteAccountID: account.ID, GroupKey: "vip", Name: "VIP"}
	mustCreatePricingRow(t, ctx, &group)
	channel := model.Channel{
		Name: "quote-group-multiplier-channel", Model: "quoted-model,no-quote-model", Enabled: true,
	}
	if err := ChannelCreate(&channel, ctx); err != nil {
		t.Fatalf("create channel: %v", err)
	}
	binding := model.SiteChannelBinding{
		SiteID: site.ID, SiteAccountID: account.ID, SiteUserGroupID: &group.ID,
		GroupKey: "vip", ChannelID: channel.ID,
	}
	mustCreatePricingRow(t, ctx, &binding)
	quote := model.SiteModelPriceQuote{
		SiteID: site.ID, SiteAccountID: &account.ID, GroupKey: "vip",
		ModelName: "quoted-model", Source: model.PriceQuoteSourceSiteExact,
		Unit: model.PriceUnitPerMillionTokens, Currency: "USD", Input: 1, Output: 2,
		GroupMultiplier: expectedMultiplier, GroupMultiplierKnown: true,
		ExchangeRateToUSD: 1, ObservedAt: time.Now(),
	}
	if err := SiteModelPriceQuotesUpsert(ctx, []model.SiteModelPriceQuote{quote}); err != nil {
		t.Fatalf("create quote: %v", err)
	}

	items, err := ChannelLLMList(ctx)
	if err != nil {
		t.Fatalf("ChannelLLMList: %v", err)
	}
	seen := 0
	for _, item := range items {
		if item.ChannelID != channel.ID {
			continue
		}
		seen++
		if item.GroupMultiplier == nil || *item.GroupMultiplier != expectedMultiplier {
			t.Fatalf("sibling quote multiplier missing from model %q: %+v", item.Name, item)
		}
	}
	if seen != 2 {
		t.Fatalf("got %d channel models, want 2", seen)
	}
}

func TestEffectivePricePreservesUnknownSiteCreditUntilRateConfigured(t *testing.T) {
	ctx := setupBackupTestDB(t)
	fixture := createPricingFixture(t, ctx, "credit")
	quote := model.SiteModelPriceQuote{
		RouteCandidateID: &fixture.candidate.ID,
		SiteID:           fixture.site.ID,
		SiteAccountID:    &fixture.account.ID,
		GroupKey:         fixture.candidate.SiteGroupKey,
		ModelName:        fixture.candidate.UpstreamModelName,
		Source:           model.PriceQuoteSourceSiteExact,
		Unit:             model.PriceUnitSiteCredit,
		Currency:         "SITE_CREDIT",
		Input:            2,
		Output:           4,
		GroupMultiplier:  2,
		ObservedAt:       time.Now(),
	}
	if err := SiteModelPriceQuotesUpsert(ctx, []model.SiteModelPriceQuote{quote}); err != nil {
		t.Fatalf("SiteModelPriceQuotesUpsert failed: %v", err)
	}
	var stored model.SiteModelPriceQuote
	if err := dbpkg.GetDB().WithContext(ctx).
		Where("site_id = ? AND model_name = ?", fixture.site.ID, fixture.candidate.UpstreamModelName).
		First(&stored).Error; err != nil {
		t.Fatalf("load stored quote failed: %v", err)
	}
	if stored.ExchangeRateToUSD != 0 {
		t.Fatalf("unknown site-credit rate was rewritten to %f", stored.ExchangeRateToUSD)
	}

	price, err := EffectivePriceForCandidate(ctx, fixture.candidate.ID, "")
	if err != nil {
		t.Fatalf("resolve site-credit price failed: %v", err)
	}
	if price.Convertible || price.ExchangeRateToUSD != 0 || price.Input != 4 || price.Output != 8 {
		t.Fatalf("unexpected unconvertible site-credit price: %+v", price)
	}

	if _, err := CurrencyRateUpsert(ctx, model.CurrencyRate{Currency: "SITE_CREDIT", RateToUSD: 0.1}); err != nil {
		t.Fatalf("CurrencyRateUpsert failed: %v", err)
	}
	price, err = EffectivePriceForCandidate(ctx, fixture.candidate.ID, "")
	if err != nil {
		t.Fatalf("resolve converted site-credit price failed: %v", err)
	}
	if !price.Convertible || math.Abs(price.ExchangeRateToUSD-0.1) > 1e-9 {
		t.Fatalf("configured site-credit rate was not applied: %+v", price)
	}
}

func TestEffectivePriceUsesBuiltInGlobalFallback(t *testing.T) {
	ctx := setupBackupTestDB(t)
	effective, err := EffectivePriceForCandidate(ctx, 0, "v0-1.0-md")
	if err != nil {
		t.Fatalf("resolve built-in global price: %v", err)
	}
	if effective.Source != model.PriceQuoteSourceGlobal ||
		!effective.Convertible ||
		effective.Input != 3 ||
		effective.Output != 15 {
		t.Fatalf("built-in global fallback mismatch: %+v", effective)
	}
}

func TestSitePriceUpsertReplacesUnboundIdentityAfterCandidateProjection(t *testing.T) {
	ctx := setupBackupTestDB(t)
	site := model.Site{
		Name: "pricing-projection-site", Platform: model.SitePlatformNewAPI,
		BaseURL: "https://pricing-projection.example.com", Enabled: true,
	}
	mustCreatePricingRow(t, ctx, &site)
	account := model.SiteAccount{
		SiteID: site.ID, Name: "pricing-projection-account",
		CredentialType: model.SiteCredentialTypeAccessToken, AccessToken: "token", Enabled: true,
	}
	mustCreatePricingRow(t, ctx, &account)
	quote := model.SiteModelPriceQuote{
		SiteID: site.ID, SiteAccountID: &account.ID, GroupKey: model.SiteDefaultGroupKey,
		ModelName: "projection-priced-model", Source: model.PriceQuoteSourceSiteExact,
		Unit: model.PriceUnitPerMillionTokens, Currency: "USD", Input: 1, Output: 2,
		GroupMultiplier: 1, ExchangeRateToUSD: 1, ObservedAt: time.Now(),
	}
	if err := SiteModelPriceQuotesUpsert(ctx, []model.SiteModelPriceQuote{quote}); err != nil {
		t.Fatalf("create unbound price quote: %v", err)
	}

	channel := model.Channel{Name: "pricing-projection-channel", Enabled: true}
	canonical := model.CanonicalModel{
		Name: "projection-priced-model", NormalizedName: "projection-priced-model", Enabled: true,
	}
	mustCreatePricingRow(t, ctx, &channel)
	mustCreatePricingRow(t, ctx, &canonical)
	candidate := model.RouteCandidate{
		CanonicalModelID: canonical.ID, ChannelID: channel.ID,
		UpstreamModelName: quote.ModelName, SiteID: &site.ID, SiteAccountID: &account.ID,
		SiteGroupKey: model.SiteDefaultGroupKey, Status: model.RouteCandidateActive,
		Weight: 1, LastSeenAt: time.Now(),
	}
	mustCreatePricingRow(t, ctx, &candidate)
	if err := SiteModelPriceQuotesUpsert(ctx, []model.SiteModelPriceQuote{quote}); err != nil {
		t.Fatalf("replace unbound quote after candidate projection: %v", err)
	}

	var stored []model.SiteModelPriceQuote
	if err := dbpkg.GetDB().WithContext(ctx).
		Where("site_id = ? AND model_name = ?", site.ID, quote.ModelName).
		Find(&stored).Error; err != nil {
		t.Fatalf("list projected price quotes: %v", err)
	}
	if len(stored) != 1 ||
		stored[0].RouteCandidateID == nil ||
		*stored[0].RouteCandidateID != candidate.ID {
		t.Fatalf("unbound quote identity was not replaced: %+v", stored)
	}
}

func TestManualCandidatePriceDoesNotLeakToSiblingCandidate(t *testing.T) {
	ctx := setupBackupTestDB(t)
	fixture := createPricingFixture(t, ctx, "manual-isolation")
	siblingChannel := model.Channel{Name: "pricing-channel-sibling", Enabled: true}
	mustCreatePricingRow(t, ctx, &siblingChannel)
	sibling := model.RouteCandidate{
		CanonicalModelID:  fixture.candidate.CanonicalModelID,
		ChannelID:         siblingChannel.ID,
		UpstreamModelName: fixture.candidate.UpstreamModelName,
		SiteID:            fixture.candidate.SiteID,
		SiteAccountID:     fixture.candidate.SiteAccountID,
		SiteGroupKey:      fixture.candidate.SiteGroupKey,
		Status:            model.RouteCandidateActive,
		Weight:            1,
		LastSeenAt:        time.Now(),
	}
	mustCreatePricingRow(t, ctx, &sibling)

	manual, err := SiteModelPriceManualUpsert(ctx, model.SiteModelPriceQuote{
		RouteCandidateID: &fixture.candidate.ID,
		Unit:             model.PriceUnitPerMillionTokens,
		Currency:         "USD",
		Input:            7,
		Output:           9,
		GroupMultiplier:  1,
	})
	if err != nil {
		t.Fatalf("create candidate-bound manual quote: %v", err)
	}
	if manual.RouteCandidateID == nil || *manual.RouteCandidateID != fixture.candidate.ID {
		t.Fatalf("manual quote lost explicit candidate binding: %+v", manual)
	}

	siblingPrice, err := EffectivePriceForCandidate(ctx, sibling.ID, "")
	if err != nil {
		t.Fatalf("resolve sibling price: %v", err)
	}
	if siblingPrice.Source != model.PriceQuoteSourceUnknown {
		t.Fatalf("candidate-bound manual quote leaked to sibling: %+v", siblingPrice)
	}

	scoped, err := SiteModelPriceManualUpsert(ctx, model.SiteModelPriceQuote{
		SiteID:          fixture.site.ID,
		SiteAccountID:   &fixture.account.ID,
		GroupKey:        fixture.candidate.SiteGroupKey,
		ModelName:       fixture.candidate.UpstreamModelName,
		Unit:            model.PriceUnitPerMillionTokens,
		Currency:        "USD",
		Input:           3,
		Output:          4,
		GroupMultiplier: 1,
	})
	if err != nil {
		t.Fatalf("create scoped manual quote: %v", err)
	}
	if scoped.RouteCandidateID != nil {
		t.Fatalf("scoped manual quote was silently bound to a candidate: %+v", scoped)
	}
	siblingPrice, err = EffectivePriceForCandidate(ctx, sibling.ID, "")
	if err != nil {
		t.Fatalf("resolve scoped sibling price: %v", err)
	}
	if siblingPrice.Source != model.PriceQuoteSourceManualOverride ||
		siblingPrice.QuoteID != scoped.ID ||
		siblingPrice.Input != 3 {
		t.Fatalf("scoped manual quote did not apply to sibling: %+v", siblingPrice)
	}
}

func TestExpiredManualPriceYieldsToFreshScopedQuote(t *testing.T) {
	ctx := setupBackupTestDB(t)
	fixture := createPricingFixture(t, ctx, "expired-manual")
	fresh := model.SiteModelPriceQuote{
		SiteID:          fixture.site.ID,
		SiteAccountID:   &fixture.account.ID,
		GroupKey:        fixture.candidate.SiteGroupKey,
		ModelName:       fixture.candidate.UpstreamModelName,
		Source:          model.PriceQuoteSourceSiteExact,
		Unit:            model.PriceUnitPerMillionTokens,
		Currency:        "USD",
		Input:           2,
		Output:          4,
		GroupMultiplier: 1,
		ObservedAt:      time.Now(),
	}
	if err := SiteModelPriceQuotesUpsert(ctx, []model.SiteModelPriceQuote{fresh}); err != nil {
		t.Fatalf("create fresh scoped quote: %v", err)
	}
	expiredAt := time.Now().Add(-time.Minute)
	manual, err := SiteModelPriceManualUpsert(ctx, model.SiteModelPriceQuote{
		RouteCandidateID: &fixture.candidate.ID,
		Unit:             model.PriceUnitPerMillionTokens,
		Currency:         "USD",
		Input:            99,
		Output:           99,
		GroupMultiplier:  1,
		ValidUntil:       &expiredAt,
	})
	if err != nil {
		t.Fatalf("create expired manual quote: %v", err)
	}

	effective, err := EffectivePriceForCandidate(ctx, fixture.candidate.ID, "")
	if err != nil {
		t.Fatalf("resolve effective price: %v", err)
	}
	if effective.Source != model.PriceQuoteSourceSiteExact ||
		effective.QuoteID == manual.ID ||
		effective.Input != 2 {
		t.Fatalf("expired manual quote still overrode fresh quote: %+v", effective)
	}

	quotes, err := SiteModelPriceQuoteList(ctx, 0, fixture.candidate.ID)
	if err != nil {
		t.Fatalf("list applicable candidate quotes: %v", err)
	}
	seenFresh := false
	seenManual := false
	for _, quote := range quotes {
		seenFresh = seenFresh || quote.Source == model.PriceQuoteSourceSiteExact
		seenManual = seenManual || quote.ID == manual.ID
	}
	if !seenFresh || !seenManual {
		t.Fatalf("candidate quote list omitted applicable or diagnostic quote: %+v", quotes)
	}
}

type pricingFixture struct {
	site      model.Site
	account   model.SiteAccount
	candidate model.RouteCandidate
}

func createPricingFixture(t *testing.T, ctx context.Context, suffix string) pricingFixture {
	t.Helper()
	site := model.Site{
		Name: "pricing-site-" + suffix, Platform: model.SitePlatformNewAPI,
		BaseURL: "https://pricing-" + suffix + ".example.com", Enabled: true,
	}
	mustCreatePricingRow(t, ctx, &site)
	account := model.SiteAccount{
		SiteID: site.ID, Name: "pricing-account-" + suffix,
		CredentialType: model.SiteCredentialTypeAccessToken, AccessToken: "token", Enabled: true,
	}
	mustCreatePricingRow(t, ctx, &account)
	channel := model.Channel{Name: "pricing-channel-" + suffix, Enabled: true}
	mustCreatePricingRow(t, ctx, &channel)
	canonical := model.CanonicalModel{
		Name: "pricing-model-" + suffix, NormalizedName: "pricing-model-" + suffix, Enabled: true,
	}
	mustCreatePricingRow(t, ctx, &canonical)
	candidate := model.RouteCandidate{
		CanonicalModelID: canonical.ID, ChannelID: channel.ID,
		UpstreamModelName: "provider/pricing-model-" + suffix,
		SiteID:            &site.ID, SiteAccountID: &account.ID, SiteGroupKey: "premium",
		Status: model.RouteCandidateActive, Weight: 1, LastSeenAt: time.Now(),
	}
	mustCreatePricingRow(t, ctx, &candidate)
	return pricingFixture{site: site, account: account, candidate: candidate}
}

func mustCreatePricingRow(t *testing.T, ctx context.Context, item any) {
	t.Helper()
	if err := dbpkg.GetDB().WithContext(ctx).Create(item).Error; err != nil {
		t.Fatalf("create pricing fixture row failed: %v", err)
	}
}
