package op

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/bestruirui/octopus/internal/db"
	"github.com/bestruirui/octopus/internal/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const (
	usageAggregateBatchSize = 1000
	usageBackfillBatchSize  = 500
)

var usageLatencyBucketUpperBounds = []int64{
	100, 250, 500, 1000, 2000, 5000, 10_000,
	30_000, 60_000, 120_000, 300_000, 600_000,
}

var usageAggregateLock sync.Mutex

type usageAggregateFact struct {
	time             int64
	scope            UsageMetricScope
	siteID           int
	siteName         string
	siteAccountID    int
	siteAccountName  string
	channelID        int
	channelName      string
	apiKeyID         int
	apiKeyName       string
	requestModel     string
	actualModel      string
	canonicalModel   string
	outcome          model.RequestOutcome
	inputTokens      int64
	outputTokens     int64
	cacheReadTokens  int64
	cacheWriteTokens int64
	costUSD          float64
	durationMS       int64
	ftutMS           int64
	ftutKnown        bool
	tokenSource      model.UsageValueSource
	priceSource      model.PriceQuoteSource
	priceConvertible bool
}

func UsageAggregatePending(ctx context.Context, batchSize int) (int, error) {
	usageAggregateLock.Lock()
	defer usageAggregateLock.Unlock()

	if batchSize <= 0 || batchSize > usageAggregateBatchSize {
		batchSize = usageAggregateBatchSize
	}
	requestCount, err := aggregateUsageRequestFacts(ctx, batchSize)
	if err != nil {
		return 0, err
	}
	attemptCount, err := aggregateUsageAttemptFacts(ctx, batchSize)
	return requestCount + attemptCount, err
}

func aggregateUsageRequestFacts(ctx context.Context, limit int) (int, error) {
	processed := 0
	err := db.GetDB().WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var facts []model.UsageRequestFact
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("aggregated_at IS NULL").
			Order("time ASC, relay_log_id ASC").
			Limit(limit).
			Find(&facts).Error; err != nil {
			return err
		}
		if len(facts) == 0 {
			return nil
		}
		deltas := make(map[string]*model.UsageAggregate)
		ids := make([]int64, 0, len(facts))
		for i := range facts {
			fact := usageAggregateFactFromRequest(facts[i])
			addUsageAggregateDeltas(deltas, fact)
			ids = append(ids, facts[i].RelayLogID)
		}
		if err := persistUsageAggregateDeltas(tx, deltas); err != nil {
			return err
		}
		now := time.Now()
		if err := tx.Model(&model.UsageRequestFact{}).
			Where("relay_log_id IN ? AND aggregated_at IS NULL", ids).
			Update("aggregated_at", now).Error; err != nil {
			return err
		}
		processed = len(facts)
		return nil
	})
	return processed, err
}

func aggregateUsageAttemptFacts(ctx context.Context, limit int) (int, error) {
	processed := 0
	err := db.GetDB().WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var facts []model.UsageAttemptFact
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("aggregated_at IS NULL").
			Order("time ASC, relay_log_id ASC, attempt_number ASC").
			Limit(limit).
			Find(&facts).Error; err != nil {
			return err
		}
		if len(facts) == 0 {
			return nil
		}
		deltas := make(map[string]*model.UsageAggregate)
		for i := range facts {
			addUsageAggregateDeltas(deltas, usageAggregateFactFromAttempt(facts[i]))
		}
		if err := persistUsageAggregateDeltas(tx, deltas); err != nil {
			return err
		}
		now := time.Now()
		for i := range facts {
			if err := tx.Model(&model.UsageAttemptFact{}).
				Where("relay_log_id = ? AND attempt_number = ? AND aggregated_at IS NULL",
					facts[i].RelayLogID,
					facts[i].AttemptNumber,
				).
				Update("aggregated_at", now).Error; err != nil {
				return err
			}
		}
		processed = len(facts)
		return nil
	})
	return processed, err
}

func addUsageAggregateDeltas(deltas map[string]*model.UsageAggregate, fact usageAggregateFact) {
	for _, granularity := range []model.UsageAggregateGranularity{
		model.UsageAggregateHourly,
		model.UsageAggregateDaily,
	} {
		bucketStart := usageAggregateBucketStart(fact.time, granularity)
		key := usageAggregateKey(fact, granularity, bucketStart)
		item := deltas[key]
		if item == nil {
			item = newUsageAggregate(key, fact, granularity, bucketStart)
			deltas[key] = item
		}
		addUsageFactToAggregate(item, fact)
	}
}

func persistUsageAggregateDeltas(tx *gorm.DB, deltas map[string]*model.UsageAggregate) error {
	for _, delta := range deltas {
		identity := *delta
		clearUsageAggregateMetrics(&identity)
		if err := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&identity).Error; err != nil {
			return err
		}
		var current model.UsageAggregate
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			First(&current, "aggregate_key = ?", delta.AggregateKey).Error; err != nil {
			return err
		}
		mergeUsageAggregate(&current, delta)
		if err := tx.Save(&current).Error; err != nil {
			return err
		}
	}
	return nil
}

func newUsageAggregate(
	key string,
	fact usageAggregateFact,
	granularity model.UsageAggregateGranularity,
	bucketStart int64,
) *model.UsageAggregate {
	return &model.UsageAggregate{
		AggregateKey:      key,
		Granularity:       granularity,
		MetricScope:       string(fact.scope),
		BucketStart:       bucketStart,
		SiteID:            fact.siteID,
		SiteName:          fact.siteName,
		SiteAccountID:     fact.siteAccountID,
		SiteAccountName:   fact.siteAccountName,
		ChannelID:         fact.channelID,
		ChannelName:       fact.channelName,
		APIKeyID:          fact.apiKeyID,
		APIKeyName:        fact.apiKeyName,
		RequestModel:      fact.requestModel,
		ActualModel:       fact.actualModel,
		CanonicalModel:    fact.canonicalModel,
		DurationHistogram: make([]uint64, len(usageLatencyBucketUpperBounds)+1),
		FTUTHistogram:     make([]uint64, len(usageLatencyBucketUpperBounds)+1),
	}
}

func addUsageFactToAggregate(item *model.UsageAggregate, fact usageAggregateFact) {
	item.MetricCount++
	switch fact.outcome {
	case model.RequestOutcomeSuccess:
		item.SuccessCount++
	case model.RequestOutcomeFailed:
		item.FailedCount++
	case model.RequestOutcomeClientCanceled:
		item.CanceledCount++
	default:
		item.IndeterminateCount++
	}
	item.InputTokens += fact.inputTokens
	item.OutputTokens += fact.outputTokens
	item.CacheReadTokens += fact.cacheReadTokens
	item.CacheWriteTokens += fact.cacheWriteTokens
	item.CostUSD += fact.costUSD
	item.DurationSumMS += fact.durationMS
	item.DurationMaxMS = max(item.DurationMaxMS, fact.durationMS)
	addUsageHistogramValue(item.DurationHistogram, fact.durationMS)
	if fact.ftutKnown {
		item.FTUTSumMS += fact.ftutMS
		item.FTUTMaxMS = max(item.FTUTMaxMS, fact.ftutMS)
		item.FTUTSamples++
		addUsageHistogramValue(item.FTUTHistogram, fact.ftutMS)
	}
	switch fact.tokenSource {
	case model.UsageValueSourceReported:
		item.ReportedCount++
	case model.UsageValueSourceEstimated:
		item.EstimatedCount++
	default:
		item.UnknownTokenCount++
	}
	if fact.priceSource == "" || fact.priceSource == model.PriceQuoteSourceUnknown || !fact.priceConvertible {
		item.UnknownPriceCount++
	}
}

func mergeUsageAggregate(target, delta *model.UsageAggregate) {
	target.MetricCount += delta.MetricCount
	target.SuccessCount += delta.SuccessCount
	target.FailedCount += delta.FailedCount
	target.CanceledCount += delta.CanceledCount
	target.IndeterminateCount += delta.IndeterminateCount
	target.InputTokens += delta.InputTokens
	target.OutputTokens += delta.OutputTokens
	target.CacheReadTokens += delta.CacheReadTokens
	target.CacheWriteTokens += delta.CacheWriteTokens
	target.CostUSD += delta.CostUSD
	target.DurationSumMS += delta.DurationSumMS
	target.DurationMaxMS = max(target.DurationMaxMS, delta.DurationMaxMS)
	target.DurationHistogram = mergeUsageHistograms(target.DurationHistogram, delta.DurationHistogram)
	target.FTUTSumMS += delta.FTUTSumMS
	target.FTUTMaxMS = max(target.FTUTMaxMS, delta.FTUTMaxMS)
	target.FTUTSamples += delta.FTUTSamples
	target.FTUTHistogram = mergeUsageHistograms(target.FTUTHistogram, delta.FTUTHistogram)
	target.ReportedCount += delta.ReportedCount
	target.EstimatedCount += delta.EstimatedCount
	target.UnknownTokenCount += delta.UnknownTokenCount
	target.UnknownPriceCount += delta.UnknownPriceCount
}

func clearUsageAggregateMetrics(item *model.UsageAggregate) {
	item.MetricCount = 0
	item.SuccessCount = 0
	item.FailedCount = 0
	item.CanceledCount = 0
	item.IndeterminateCount = 0
	item.InputTokens = 0
	item.OutputTokens = 0
	item.CacheReadTokens = 0
	item.CacheWriteTokens = 0
	item.CostUSD = 0
	item.DurationSumMS = 0
	item.DurationMaxMS = 0
	item.DurationHistogram = make([]uint64, len(usageLatencyBucketUpperBounds)+1)
	item.FTUTSumMS = 0
	item.FTUTMaxMS = 0
	item.FTUTSamples = 0
	item.FTUTHistogram = make([]uint64, len(usageLatencyBucketUpperBounds)+1)
	item.ReportedCount = 0
	item.EstimatedCount = 0
	item.UnknownTokenCount = 0
	item.UnknownPriceCount = 0
}

func usageAggregateFactFromRequest(item model.UsageRequestFact) usageAggregateFact {
	return usageAggregateFact{
		time: item.Time, scope: UsageMetricScopeRequest,
		siteID: item.SiteID, siteName: item.SiteName,
		siteAccountID: item.SiteAccountID, siteAccountName: item.SiteAccountName,
		channelID: item.ChannelID, channelName: item.ChannelName,
		apiKeyID: item.APIKeyID, apiKeyName: item.APIKeyName,
		requestModel: item.RequestModel, actualModel: item.ActualModel,
		canonicalModel: item.CanonicalModel, outcome: item.Outcome,
		inputTokens: item.InputTokens, outputTokens: item.OutputTokens,
		cacheReadTokens: item.CacheReadTokens, cacheWriteTokens: item.CacheWriteTokens,
		costUSD: item.CostUSD, durationMS: item.DurationMS,
		ftutMS: item.FTUTMS, ftutKnown: item.FTUTKnown,
		tokenSource: item.TokenSource, priceSource: item.PriceSource,
		priceConvertible: item.PriceConvertible,
	}
}

func usageAggregateFactFromAttempt(item model.UsageAttemptFact) usageAggregateFact {
	return usageAggregateFact{
		time: item.Time, scope: UsageMetricScopeAttempt,
		siteID: item.SiteID, siteName: item.SiteName,
		siteAccountID: item.SiteAccountID, siteAccountName: item.SiteAccountName,
		channelID: item.ChannelID, channelName: item.ChannelName,
		apiKeyID: item.APIKeyID, apiKeyName: item.APIKeyName,
		requestModel: item.RequestModel, actualModel: item.ActualModel,
		canonicalModel: item.CanonicalModel, outcome: item.Outcome,
		inputTokens: item.InputTokens, outputTokens: item.OutputTokens,
		cacheReadTokens: item.CacheReadTokens, cacheWriteTokens: item.CacheWriteTokens,
		costUSD: item.CostUSD, durationMS: item.DurationMS,
		ftutMS: item.FTUTMS, ftutKnown: item.FTUTKnown,
		tokenSource: item.TokenSource, priceSource: item.PriceSource,
		priceConvertible: item.PriceConvertible,
	}
}

func usageAggregateBucketStart(timestamp int64, granularity model.UsageAggregateGranularity) int64 {
	value := time.Unix(timestamp, 0).UTC()
	if granularity == model.UsageAggregateHourly {
		return value.Truncate(time.Hour).Unix()
	}
	return time.Date(value.Year(), value.Month(), value.Day(), 0, 0, 0, 0, time.UTC).Unix()
}

func usageAggregateKey(
	fact usageAggregateFact,
	granularity model.UsageAggregateGranularity,
	bucketStart int64,
) string {
	raw := fmt.Sprintf(
		"%s\x00%s\x00%d\x00%d\x00%d\x00%d\x00%d\x00%s\x00%s\x00%s",
		granularity,
		fact.scope,
		bucketStart,
		fact.siteID,
		fact.siteAccountID,
		fact.channelID,
		fact.apiKeyID,
		fact.requestModel,
		fact.actualModel,
		fact.canonicalModel,
	)
	sum := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(sum[:])
}

func addUsageHistogramValue(histogram []uint64, value int64) {
	if len(histogram) == 0 {
		return
	}
	for index, upper := range usageLatencyBucketUpperBounds {
		if value <= upper {
			histogram[index]++
			return
		}
	}
	histogram[len(histogram)-1]++
}

func mergeUsageHistograms(left, right []uint64) []uint64 {
	size := max(len(left), len(right))
	result := make([]uint64, size)
	for index := 0; index < size; index++ {
		if index < len(left) {
			result[index] += left[index]
		}
		if index < len(right) {
			result[index] += right[index]
		}
	}
	return result
}

func usageHistogramPercentile95(histogram []uint64, maximum int64) int64 {
	var count uint64
	for _, value := range histogram {
		count += value
	}
	if count == 0 {
		return 0
	}
	target := (count*95 + 99) / 100
	var cumulative uint64
	for index, value := range histogram {
		cumulative += value
		if cumulative < target {
			continue
		}
		if index < len(usageLatencyBucketUpperBounds) {
			return usageLatencyBucketUpperBounds[index]
		}
		return maximum
	}
	return maximum
}

func UsageFactsBackfill(ctx context.Context, limit int) (int, error) {
	if limit <= 0 || limit > usageBackfillBatchSize {
		limit = usageBackfillBatchSize
	}
	var logs []model.RelayLog
	subquery := db.GetDB().Model(&model.UsageRequestFact{}).
		Select("1").
		Where("usage_request_facts.relay_log_id = relay_logs.id")
	if err := db.GetDB().WithContext(ctx).
		Where("NOT EXISTS (?)", subquery).
		Order("id ASC").
		Limit(limit).
		Find(&logs).Error; err != nil {
		return 0, err
	}
	if len(logs) == 0 {
		return 0, nil
	}
	batch := make([]usagePendingRecord, 0, len(logs))
	for i := range logs {
		batch = append(batch, usageFactsFromRelayLog(logs[i]))
	}
	if err := enrichUsageDimensions(ctx, batch); err != nil {
		return 0, err
	}
	err := db.GetDB().WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		requests := make([]model.UsageRequestFact, 0, len(batch))
		attempts := make([]model.UsageAttemptFact, 0, len(batch)*2)
		for i := range batch {
			requests = append(requests, batch[i].request)
			attempts = append(attempts, batch[i].attempts...)
		}
		if err := tx.Clauses(clause.OnConflict{DoNothing: true}).
			CreateInBatches(&requests, usageFactBatchSize).Error; err != nil {
			return err
		}
		if len(attempts) > 0 {
			return tx.Clauses(clause.OnConflict{DoNothing: true}).
				CreateInBatches(&attempts, usageFactBatchSize).Error
		}
		return nil
	})
	return len(logs), err
}

func UsageAggregateRetention(ctx context.Context, now time.Time, hourlyDays int) (int64, error) {
	cutoff := usageRetentionCutoff(now, hourlyDays)
	result := db.GetDB().WithContext(ctx).
		Where("granularity = ? AND bucket_start < ?", model.UsageAggregateHourly, cutoff).
		Delete(&model.UsageAggregate{})
	return result.RowsAffected, result.Error
}

func UsageFactsRetention(
	ctx context.Context,
	now time.Time,
	hourlyDays int,
	batchSize int,
) (int64, error) {
	if batchSize <= 0 || batchSize > usageAggregateBatchSize {
		batchSize = usageAggregateBatchSize
	}
	cutoff := usageRetentionCutoff(now, hourlyDays)
	var deleted int64
	err := db.GetDB().WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var requestIDs []int64
		if err := tx.Model(&model.UsageRequestFact{}).
			Where("aggregated_at IS NOT NULL AND time < ?", cutoff).
			Order("time ASC, relay_log_id ASC").
			Limit(batchSize).
			Pluck("relay_log_id", &requestIDs).Error; err != nil {
			return err
		}
		if len(requestIDs) > 0 {
			result := tx.Where("relay_log_id IN ? AND aggregated_at IS NOT NULL", requestIDs).
				Delete(&model.UsageRequestFact{})
			if result.Error != nil {
				return result.Error
			}
			deleted += result.RowsAffected
		}

		type attemptKey struct {
			RelayLogID    int64
			AttemptNumber int
		}
		var attemptKeys []attemptKey
		if err := tx.Model(&model.UsageAttemptFact{}).
			Select("relay_log_id, attempt_number").
			Where("aggregated_at IS NOT NULL AND time < ?", cutoff).
			Order("time ASC, relay_log_id ASC, attempt_number ASC").
			Limit(batchSize).
			Scan(&attemptKeys).Error; err != nil {
			return err
		}
		for _, key := range attemptKeys {
			result := tx.Where(
				"relay_log_id = ? AND attempt_number = ? AND aggregated_at IS NOT NULL",
				key.RelayLogID,
				key.AttemptNumber,
			).Delete(&model.UsageAttemptFact{})
			if result.Error != nil {
				return result.Error
			}
			deleted += result.RowsAffected
		}
		return nil
	})
	return deleted, err
}

func usageRetentionCutoff(now time.Time, hourlyDays int) int64 {
	if hourlyDays <= 0 {
		hourlyDays = usageHourlyRetentionDays()
	}
	return now.UTC().AddDate(0, 0, -hourlyDays).Truncate(time.Hour).Unix()
}

func usageHourlyRetentionDays() int {
	value, err := SettingGetInt(model.SettingKeyUsageHourlyRetentionDays)
	if err != nil || value <= 0 {
		return 90
	}
	return value
}

func normalizeUsageAggregateName(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return "Unmanaged"
	}
	return value
}
