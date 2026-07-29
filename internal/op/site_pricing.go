package op

import (
	"context"
	"fmt"
	"math"
	"sort"
	"strings"
	"time"

	"github.com/bestruirui/octopus/internal/db"
	"github.com/bestruirui/octopus/internal/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const sitePriceFreshness = 24 * time.Hour

func SiteModelPriceQuotesUpsert(ctx context.Context, quotes []model.SiteModelPriceQuote) error {
	if len(quotes) == 0 {
		return nil
	}
	for i := range quotes {
		if err := normalizeSiteModelPriceQuote(ctx, &quotes[i]); err != nil {
			return err
		}
		if err := db.GetDB().WithContext(ctx).Clauses(clause.OnConflict{
			Columns: []clause.Column{{Name: "identity_key"}},
			DoUpdates: clause.AssignmentColumns([]string{
				"route_candidate_id",
				"site_id",
				"site_account_id",
				"group_key",
				"model_name",
				"source",
				"unit",
				"currency",
				"input",
				"output",
				"cache_read",
				"cache_write",
				"per_request",
				"group_multiplier",
				"exchange_rate_to_usd",
				"raw_payload",
				"observed_at",
				"valid_until",
				"manual_override",
				"status",
				"last_error",
				"updated_at",
			}),
		}).Create(&quotes[i]).Error; err != nil {
			return err
		}
	}
	return nil
}

func normalizeSiteModelPriceQuote(ctx context.Context, quote *model.SiteModelPriceQuote) error {
	if quote == nil {
		return fmt.Errorf("price quote is nil")
	}
	if err := hydratePriceQuoteScope(ctx, quote); err != nil {
		return err
	}
	quote.ModelName = strings.TrimSpace(quote.ModelName)
	quote.GroupKey = model.NormalizeSiteGroupKey(quote.GroupKey)
	if quote.Source == "" {
		quote.Source = model.PriceQuoteSourceSiteExact
	}
	if quote.Unit == "" {
		quote.Unit = model.PriceUnitPerMillionTokens
	}
	quote.Currency = strings.ToUpper(strings.TrimSpace(quote.Currency))
	if quote.Unit == model.PriceUnitSiteCredit && quote.Currency == "" {
		quote.Currency = "SITE_CREDIT"
	}
	if quote.Currency == "" {
		quote.Currency = "USD"
	}
	if quote.GroupMultiplier == 0 {
		quote.GroupMultiplier = 1
	}
	if quote.ExchangeRateToUSD == 0 {
		if rate, ok := currencyRateToUSD(ctx, quote.Currency); ok {
			quote.ExchangeRateToUSD = rate
		} else {
			quote.ExchangeRateToUSD = 0
		}
	}
	if quote.ObservedAt.IsZero() {
		quote.ObservedAt = time.Now()
	}
	if quote.SiteID <= 0 || quote.ModelName == "" {
		return fmt.Errorf("site and model are required for price quote")
	}
	if validationErr := validateSiteModelPriceQuote(*quote); validationErr != nil {
		quote.Status = model.PriceQuoteStatusRejected
		quote.LastError = validationErr.Error()
	} else {
		quote.Status = model.PriceQuoteStatusValid
		quote.LastError = ""
	}
	if err := linkPriceQuoteRouteCandidate(ctx, quote); err != nil {
		return err
	}
	quote.RefreshIdentityKey()
	return nil
}

func hydratePriceQuoteScope(ctx context.Context, quote *model.SiteModelPriceQuote) error {
	if quote == nil || quote.RouteCandidateID == nil || *quote.RouteCandidateID <= 0 {
		return nil
	}
	var candidate model.RouteCandidate
	if err := db.GetDB().WithContext(ctx).First(&candidate, *quote.RouteCandidateID).Error; err != nil {
		return err
	}
	if quote.SiteID <= 0 && candidate.SiteID != nil {
		quote.SiteID = *candidate.SiteID
	}
	if quote.SiteAccountID == nil && candidate.SiteAccountID != nil {
		accountID := *candidate.SiteAccountID
		quote.SiteAccountID = &accountID
	}
	if strings.TrimSpace(quote.GroupKey) == "" {
		quote.GroupKey = candidate.SiteGroupKey
	}
	if strings.TrimSpace(quote.ModelName) == "" {
		quote.ModelName = candidate.UpstreamModelName
	}
	return nil
}

func validateSiteModelPriceQuote(quote model.SiteModelPriceQuote) error {
	switch quote.Unit {
	case model.PriceUnitPerMillionTokens, model.PriceUnitPerRequest, model.PriceUnitSiteCredit:
	default:
		return fmt.Errorf("unsupported price unit %q", quote.Unit)
	}
	for name, value := range map[string]float64{
		"input":            quote.Input,
		"output":           quote.Output,
		"cache_read":       quote.CacheRead,
		"cache_write":      quote.CacheWrite,
		"per_request":      quote.PerRequest,
		"group_multiplier": quote.GroupMultiplier,
		"exchange_rate":    quote.ExchangeRateToUSD,
	} {
		if math.IsNaN(value) || math.IsInf(value, 0) || value < 0 {
			return fmt.Errorf("%s must be a finite non-negative number", name)
		}
	}
	return nil
}

func linkPriceQuoteRouteCandidate(ctx context.Context, quote *model.SiteModelPriceQuote) error {
	if quote == nil || quote.RouteCandidateID != nil || quote.SiteAccountID == nil {
		return nil
	}
	var candidate model.RouteCandidate
	query := db.GetDB().WithContext(ctx).
		Where(
			"site_account_id = ? AND LOWER(upstream_model_name) = ?",
			*quote.SiteAccountID,
			strings.ToLower(quote.ModelName),
		)
	if quote.GroupKey != "" {
		query = query.Where("site_group_key = ?", model.NormalizeSiteGroupKey(quote.GroupKey))
	}
	err := query.Order("id ASC").First(&candidate).Error
	if err == nil {
		quote.RouteCandidateID = &candidate.ID
		return nil
	}
	if err == gorm.ErrRecordNotFound {
		return nil
	}
	return err
}

func SiteModelPriceQuoteList(ctx context.Context, canonicalModelID, routeCandidateID int) ([]model.SiteModelPriceQuote, error) {
	query := db.GetDB().WithContext(ctx).Model(&model.SiteModelPriceQuote{})
	if routeCandidateID > 0 {
		query = query.Where("route_candidate_id = ?", routeCandidateID)
	} else if canonicalModelID > 0 {
		query = query.Where("route_candidate_id IN (?)",
			db.GetDB().Model(&model.RouteCandidate{}).Select("id").Where("canonical_model_id = ?", canonicalModelID),
		)
	}
	var items []model.SiteModelPriceQuote
	err := query.Order("manual_override DESC, observed_at DESC, id DESC").Find(&items).Error
	return items, err
}

func SiteModelPriceManualUpsert(ctx context.Context, quote model.SiteModelPriceQuote) (*model.SiteModelPriceQuote, error) {
	quote.Source = model.PriceQuoteSourceManualOverride
	quote.ManualOverride = true
	quote.ObservedAt = time.Now()
	if err := normalizeSiteModelPriceQuote(ctx, &quote); err != nil {
		return nil, err
	}
	if quote.Status == model.PriceQuoteStatusRejected {
		return nil, fmt.Errorf("invalid manual price quote: %s", quote.LastError)
	}
	if err := SiteModelPriceQuotesUpsert(ctx, []model.SiteModelPriceQuote{quote}); err != nil {
		return nil, err
	}
	var saved model.SiteModelPriceQuote
	if err := db.GetDB().WithContext(ctx).
		Where("identity_key = ?", quote.IdentityKey).
		First(&saved).Error; err != nil {
		return nil, err
	}
	return &saved, nil
}

func SiteModelPriceManualDelete(ctx context.Context, id int) error {
	if id <= 0 {
		return fmt.Errorf("price quote id is required")
	}
	result := db.GetDB().WithContext(ctx).
		Where("id = ? AND manual_override = ?", id, true).
		Delete(&model.SiteModelPriceQuote{})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return fmt.Errorf("manual price quote not found")
	}
	return nil
}

func SiteModelPriceQuoteMarkRefreshError(ctx context.Context, siteID, accountID int, refreshErr error) error {
	if siteID <= 0 || accountID <= 0 || refreshErr == nil {
		return nil
	}
	return db.GetDB().WithContext(ctx).
		Model(&model.SiteModelPriceQuote{}).
		Where("site_id = ? AND site_account_id = ? AND manual_override = ?", siteID, accountID, false).
		Update("last_error", refreshErr.Error()).Error
}

func SiteModelPriceQuoteClearRefreshError(ctx context.Context, siteID, accountID int) error {
	if siteID <= 0 || accountID <= 0 {
		return nil
	}
	return db.GetDB().WithContext(ctx).
		Model(&model.SiteModelPriceQuote{}).
		Where("site_id = ? AND site_account_id = ? AND manual_override = ?", siteID, accountID, false).
		Update("last_error", "").Error
}

func EffectivePriceForCandidate(ctx context.Context, candidateID int, fallbackModel string) (model.EffectivePrice, error) {
	var candidate model.RouteCandidate
	if candidateID > 0 {
		if err := db.GetDB().WithContext(ctx).First(&candidate, candidateID).Error; err != nil && err != gorm.ErrRecordNotFound {
			return model.EffectivePrice{}, err
		}
	}
	modelName := strings.TrimSpace(candidate.UpstreamModelName)
	if modelName == "" {
		modelName = strings.TrimSpace(fallbackModel)
	}

	quotes, err := matchingPriceQuotes(ctx, candidate, modelName)
	if err != nil {
		return model.EffectivePrice{}, err
	}
	if len(quotes) > 0 {
		now := time.Now()
		sort.SliceStable(quotes, func(i, j int) bool {
			leftRank := priceQuoteRank(quotes[i], candidate, now)
			rightRank := priceQuoteRank(quotes[j], candidate, now)
			if leftRank == rightRank {
				if quotes[i].ObservedAt.Equal(quotes[j].ObservedAt) {
					return quotes[i].ID > quotes[j].ID
				}
				return quotes[i].ObservedAt.After(quotes[j].ObservedAt)
			}
			return leftRank > rightRank
		})
		selected := quotes[0]
		fresh := priceQuoteFresh(selected, now)
		source, reason := effectiveQuoteSource(selected, candidate, fresh)
		return effectivePriceFromQuote(ctx, selected, candidate.ID, source, !fresh, reason), nil
	}

	if global, err := LLMGet(strings.ToLower(modelName)); err == nil {
		return model.EffectivePrice{
			RouteCandidateID:  candidateID,
			Source:            model.PriceQuoteSourceGlobal,
			Unit:              model.PriceUnitPerMillionTokens,
			Currency:          "USD",
			Input:             global.Input,
			Output:            global.Output,
			CacheRead:         global.CacheRead,
			CacheWrite:        global.CacheWrite,
			GroupMultiplier:   1,
			ExchangeRateToUSD: 1,
			Convertible:       true,
			MatchReason:       "global model fallback",
		}, nil
	}
	return model.EffectivePrice{
		RouteCandidateID: candidateID,
		Source:           model.PriceQuoteSourceUnknown,
		Unit:             model.PriceUnitPerMillionTokens,
		Currency:         "USD",
		GroupMultiplier:  1,
		Convertible:      false,
		MatchReason:      "no matching site or global price",
	}, nil
}

func matchingPriceQuotes(
	ctx context.Context,
	candidate model.RouteCandidate,
	modelName string,
) ([]model.SiteModelPriceQuote, error) {
	if candidate.ID <= 0 {
		return nil, nil
	}
	query := db.GetDB().WithContext(ctx).
		Where("LOWER(model_name) = ? AND status = ?", strings.ToLower(strings.TrimSpace(modelName)), model.PriceQuoteStatusValid)
	if candidate.SiteID != nil {
		query = query.Where("route_candidate_id = ? OR site_id = ?", candidate.ID, *candidate.SiteID)
	} else {
		query = query.Where("route_candidate_id = ?", candidate.ID)
	}
	var quotes []model.SiteModelPriceQuote
	if err := query.Find(&quotes).Error; err != nil {
		return nil, err
	}
	filtered := quotes[:0]
	for _, quote := range quotes {
		if priceQuoteMatchesCandidate(quote, candidate) {
			filtered = append(filtered, quote)
		}
	}
	return filtered, nil
}

func priceQuoteMatchesCandidate(quote model.SiteModelPriceQuote, candidate model.RouteCandidate) bool {
	if quote.RouteCandidateID != nil && *quote.RouteCandidateID == candidate.ID {
		return true
	}
	if candidate.SiteID == nil || quote.SiteID != *candidate.SiteID {
		return false
	}
	if quote.SiteAccountID != nil {
		if candidate.SiteAccountID == nil || *quote.SiteAccountID != *candidate.SiteAccountID {
			return false
		}
	}
	group := model.NormalizeSiteGroupKey(quote.GroupKey)
	candidateGroup := model.NormalizeSiteGroupKey(candidate.SiteGroupKey)
	return group == candidateGroup || group == model.SiteDefaultGroupKey
}

func priceQuoteRank(quote model.SiteModelPriceQuote, candidate model.RouteCandidate, now time.Time) int {
	specificity := priceQuoteSpecificity(quote, candidate)
	if quote.ManualOverride || quote.Source == model.PriceQuoteSourceManualOverride {
		return 1000 + specificity
	}
	if priceQuoteFresh(quote, now) {
		return 500 + specificity
	}
	return 100 + specificity
}

func priceQuoteSpecificity(quote model.SiteModelPriceQuote, candidate model.RouteCandidate) int {
	if quote.RouteCandidateID != nil && *quote.RouteCandidateID == candidate.ID {
		return 40
	}
	groupExact := model.NormalizeSiteGroupKey(quote.GroupKey) == model.NormalizeSiteGroupKey(candidate.SiteGroupKey)
	if quote.SiteAccountID != nil {
		if groupExact {
			return 30
		}
		return 20
	}
	if groupExact {
		return 10
	}
	return 0
}

func priceQuoteFresh(quote model.SiteModelPriceQuote, now time.Time) bool {
	if quote.ValidUntil != nil && !now.Before(*quote.ValidUntil) {
		return false
	}
	if quote.ManualOverride {
		return true
	}
	return !quote.ObservedAt.IsZero() && now.Sub(quote.ObservedAt) <= sitePriceFreshness
}

func effectiveQuoteSource(
	quote model.SiteModelPriceQuote,
	candidate model.RouteCandidate,
	fresh bool,
) (model.PriceQuoteSource, string) {
	specificity := priceQuoteSpecificity(quote, candidate)
	if quote.ManualOverride || quote.Source == model.PriceQuoteSourceManualOverride {
		return model.PriceQuoteSourceManualOverride, "manual route or scoped override"
	}
	if !fresh {
		return model.PriceQuoteSourceSiteStale, "last known site quote is stale"
	}
	if specificity >= 30 {
		return model.PriceQuoteSourceSiteExact, "exact route candidate or account/group quote"
	}
	return model.PriceQuoteSourceSiteWide, "wider site/default-group quote"
}

func effectivePriceFromQuote(
	ctx context.Context,
	quote model.SiteModelPriceQuote,
	candidateID int,
	source model.PriceQuoteSource,
	stale bool,
	reason string,
) model.EffectivePrice {
	observedAt := quote.ObservedAt
	multiplier := quote.GroupMultiplier
	if multiplier <= 0 {
		multiplier = 1
	}
	rate := quote.ExchangeRateToUSD
	if rate <= 0 {
		if configuredRate, ok := currencyRateToUSD(ctx, quote.Currency); ok {
			rate = configuredRate
		}
	}
	return model.EffectivePrice{
		QuoteID:           quote.ID,
		RouteCandidateID:  candidateID,
		Source:            source,
		Unit:              quote.Unit,
		Currency:          quote.Currency,
		Input:             quote.Input * multiplier,
		Output:            quote.Output * multiplier,
		CacheRead:         quote.CacheRead * multiplier,
		CacheWrite:        quote.CacheWrite * multiplier,
		PerRequest:        quote.PerRequest * multiplier,
		GroupMultiplier:   multiplier,
		ExchangeRateToUSD: rate,
		ObservedAt:        &observedAt,
		Stale:             stale,
		Convertible:       rate > 0,
		MatchReason:       reason,
	}
}

func CurrencyRateList(ctx context.Context) ([]model.CurrencyRate, error) {
	var items []model.CurrencyRate
	err := db.GetDB().WithContext(ctx).Order("currency ASC").Find(&items).Error
	return items, err
}

func CurrencyRateUpsert(ctx context.Context, item model.CurrencyRate) (*model.CurrencyRate, error) {
	item.Currency = strings.ToUpper(strings.TrimSpace(item.Currency))
	if item.Currency == "" || item.RateToUSD <= 0 || math.IsNaN(item.RateToUSD) || math.IsInf(item.RateToUSD, 0) {
		return nil, fmt.Errorf("currency and finite positive rate_to_usd are required")
	}
	if item.Currency == "USD" {
		item.RateToUSD = 1
	}
	if err := db.GetDB().WithContext(ctx).Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "currency"}},
		DoUpdates: clause.AssignmentColumns([]string{"rate_to_usd", "updated_at"}),
	}).Create(&item).Error; err != nil {
		return nil, err
	}
	var saved model.CurrencyRate
	if err := db.GetDB().WithContext(ctx).First(&saved, "currency = ?", item.Currency).Error; err != nil {
		return nil, err
	}
	return &saved, nil
}

func currencyRateToUSD(ctx context.Context, currency string) (float64, bool) {
	if strings.EqualFold(currency, "USD") {
		return 1, true
	}
	var item model.CurrencyRate
	if err := db.GetDB().WithContext(ctx).
		First(&item, "currency = ?", strings.ToUpper(strings.TrimSpace(currency))).Error; err == nil && item.RateToUSD > 0 {
		return item.RateToUSD, true
	}
	return 0, false
}
