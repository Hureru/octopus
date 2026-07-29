package op

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/bestruirui/octopus/internal/db"
	"github.com/bestruirui/octopus/internal/model"
	"gorm.io/gorm"
)

type usageAnalyticsAggregateWindow struct {
	start       int64
	end         int64
	granularity model.UsageAggregateGranularity
}

func (window usageAnalyticsAggregateWindow) valid() bool {
	return window.end > window.start
}

type usageAnalyticsAccumulator struct {
	row             usageAggregateRow
	durationHist    []uint64
	durationMaximum int64
	ftutHist        []uint64
	ftutMaximum     int64
}

func usageAnalyticsSummaryAccumulator(
	ctx context.Context,
	filter UsageAnalyticsFilter,
) (usageAnalyticsAccumulator, error) {
	window := usageAnalyticsDailyAggregateWindow(filter)
	factQuery := usageAnalyticsFactQuery(ctx, filter, window)
	var row usageAggregateRow
	if err := factQuery.Session(&gorm.Session{}).Select(`
		COUNT(*) AS metric_count,
		COALESCE(SUM(CASE WHEN outcome = ? THEN 1 ELSE 0 END), 0) AS success_count,
		COALESCE(SUM(CASE WHEN outcome = ? THEN 1 ELSE 0 END), 0) AS failed_count,
		COALESCE(SUM(CASE WHEN outcome = ? THEN 1 ELSE 0 END), 0) AS canceled_count,
		COALESCE(SUM(CASE WHEN outcome = ? THEN 1 ELSE 0 END), 0) AS indeterminate_count,
		COALESCE(SUM(input_tokens), 0) AS input_tokens,
		COALESCE(SUM(output_tokens), 0) AS output_tokens,
		COALESCE(SUM(cache_read_tokens), 0) AS cache_read_tokens,
		COALESCE(SUM(cache_write_tokens), 0) AS cache_write_tokens,
		COALESCE(SUM(cost_usd), 0) AS cost_usd,
		COALESCE(SUM(duration_ms), 0) AS duration_sum_ms,
		COALESCE(SUM(CASE WHEN ftut_known = ? THEN ftut_ms ELSE 0 END), 0) AS ftut_sum_ms,
		COALESCE(SUM(CASE WHEN ftut_known = ? THEN 1 ELSE 0 END), 0) AS ftut_samples`,
		model.RequestOutcomeSuccess,
		model.RequestOutcomeFailed,
		model.RequestOutcomeClientCanceled,
		model.RequestOutcomeIndeterminate,
		true,
		true,
	).Scan(&row).Error; err != nil {
		return usageAnalyticsAccumulator{}, err
	}

	durationHist, durationMax, err := usageHistogramFromQuery(
		factQuery.Session(&gorm.Session{}),
		"duration_ms",
		false,
	)
	if err != nil {
		return usageAnalyticsAccumulator{}, err
	}
	ftutHist, ftutMax, err := usageHistogramFromQuery(
		factQuery.Session(&gorm.Session{}),
		"ftut_ms",
		true,
	)
	if err != nil {
		return usageAnalyticsAccumulator{}, err
	}
	accumulator := usageAnalyticsAccumulator{
		row:             row,
		durationHist:    durationHist,
		durationMaximum: durationMax,
		ftutHist:        ftutHist,
		ftutMaximum:     ftutMax,
	}

	if window.valid() {
		var aggregates []model.UsageAggregate
		if err := usageAnalyticsAggregateQuery(ctx, filter, window).Find(&aggregates).Error; err != nil {
			return usageAnalyticsAccumulator{}, err
		}
		for i := range aggregates {
			accumulator.addAggregate(aggregates[i])
		}
	}
	return accumulator, nil
}

func (accumulator *usageAnalyticsAccumulator) addAggregate(item model.UsageAggregate) {
	accumulator.row.MetricCount += item.MetricCount
	accumulator.row.SuccessCount += item.SuccessCount
	accumulator.row.FailedCount += item.FailedCount
	accumulator.row.CanceledCount += item.CanceledCount
	accumulator.row.IndeterminateCount += item.IndeterminateCount
	accumulator.row.InputTokens += item.InputTokens
	accumulator.row.OutputTokens += item.OutputTokens
	accumulator.row.CacheReadTokens += item.CacheReadTokens
	accumulator.row.CacheWriteTokens += item.CacheWriteTokens
	accumulator.row.CostUSD += item.CostUSD
	accumulator.row.DurationSumMS += item.DurationSumMS
	accumulator.row.FTUTSumMS += item.FTUTSumMS
	accumulator.row.FTUTSamples += item.FTUTSamples
	accumulator.durationHist = mergeUsageHistograms(accumulator.durationHist, item.DurationHistogram)
	accumulator.durationMaximum = max(accumulator.durationMaximum, item.DurationMaxMS)
	accumulator.ftutHist = mergeUsageHistograms(accumulator.ftutHist, item.FTUTHistogram)
	accumulator.ftutMaximum = max(accumulator.ftutMaximum, item.FTUTMaxMS)
}

func (accumulator usageAnalyticsAccumulator) metrics(scope UsageMetricScope) UsageAnalyticsMetrics {
	metrics := usageMetricsFromAggregate(accumulator.row, scope)
	metrics.P95DurationMS = usageHistogramPercentile95(
		accumulator.durationHist,
		accumulator.durationMaximum,
	)
	metrics.P95FTUTMS = usageHistogramPercentile95(
		accumulator.ftutHist,
		accumulator.ftutMaximum,
	)
	return metrics
}

func usageAnalyticsDailyAggregateWindow(filter UsageAnalyticsFilter) usageAnalyticsAggregateWindow {
	cutoff := time.Now().UTC().
		AddDate(0, 0, -usageHourlyRetentionDays()).
		Truncate(time.Hour).
		Unix()
	upper := min(filter.EndTime, cutoff)
	if upper <= filter.StartTime {
		return usageAnalyticsAggregateWindow{}
	}

	startValue := time.Unix(filter.StartTime, 0).UTC()
	start := time.Date(
		startValue.Year(),
		startValue.Month(),
		startValue.Day(),
		0, 0, 0, 0,
		time.UTC,
	)
	if start.Unix() < filter.StartTime {
		start = start.AddDate(0, 0, 1)
	}
	endValue := time.Unix(upper, 0).UTC()
	end := time.Date(
		endValue.Year(),
		endValue.Month(),
		endValue.Day(),
		0, 0, 0, 0,
		time.UTC,
	)
	return usageAnalyticsAggregateWindow{
		start:       start.Unix(),
		end:         end.Unix(),
		granularity: model.UsageAggregateDaily,
	}
}

func usageAnalyticsHourlyAggregateWindow(filter UsageAnalyticsFilter) usageAnalyticsAggregateWindow {
	retentionCutoff := time.Now().UTC().
		AddDate(0, 0, -usageHourlyRetentionDays()).
		Truncate(time.Hour)
	start := time.Unix(filter.StartTime, 0).UTC().Truncate(time.Hour)
	if start.Unix() < filter.StartTime {
		start = start.Add(time.Hour)
	}
	if start.Before(retentionCutoff) {
		start = retentionCutoff
	}
	end := time.Unix(filter.EndTime, 0).UTC().Truncate(time.Hour)
	currentHour := time.Now().UTC().Truncate(time.Hour)
	if end.After(currentHour) {
		end = currentHour
	}
	return usageAnalyticsAggregateWindow{
		start:       start.Unix(),
		end:         end.Unix(),
		granularity: model.UsageAggregateHourly,
	}
}

func usageAnalyticsFactQuery(
	ctx context.Context,
	filter UsageAnalyticsFilter,
	window usageAnalyticsAggregateWindow,
) *gorm.DB {
	query := usageAnalyticsBaseQuery(ctx, filter)
	if window.valid() {
		// Aggregates cover only facts that have been transactionally
		// marked as aggregated. Keep pending facts visible during backfill so a
		// retention-window boundary cannot temporarily hide usage.
		query = query.Where(
			"time < ? OR time >= ? OR aggregated_at IS NULL",
			window.start,
			window.end,
		)
	}
	return query
}

func usageAnalyticsAggregateQuery(
	ctx context.Context,
	filter UsageAnalyticsFilter,
	window usageAnalyticsAggregateWindow,
) *gorm.DB {
	query := dbForUsageAnalytics(ctx).
		Model(&model.UsageAggregate{}).
		Where(
			"granularity = ? AND metric_scope = ? AND bucket_start >= ? AND bucket_start < ?",
			window.granularity,
			filter.Scope,
			window.start,
			window.end,
		)
	if len(filter.SiteIDs) > 0 {
		query = query.Where("site_id IN ?", filter.SiteIDs)
	}
	if len(filter.SiteAccountIDs) > 0 {
		query = query.Where("site_account_id IN ?", filter.SiteAccountIDs)
	}
	if len(filter.ChannelIDs) > 0 {
		query = query.Where("channel_id IN ?", filter.ChannelIDs)
	}
	if len(filter.APIKeyIDs) > 0 {
		query = query.Where("api_key_id IN ?", filter.APIKeyIDs)
	}
	if len(filter.RequestModels) > 0 {
		query = query.Where("request_model IN ?", filter.RequestModels)
	}
	if len(filter.ActualModels) > 0 {
		query = query.Where("actual_model IN ?", filter.ActualModels)
	}
	if len(filter.CanonicalModels) > 0 {
		query = query.Where("canonical_model IN ?", filter.CanonicalModels)
	}
	return query
}

func usageHistogramFromQuery(
	query *gorm.DB,
	column string,
	requireFTUT bool,
) ([]uint64, int64, error) {
	expression, err := usageHistogramBucketExpression(column)
	if err != nil {
		return nil, 0, err
	}
	if requireFTUT {
		query = query.Where("ftut_known = ?", true)
	}
	var rows []struct {
		BucketIndex int
		BucketCount int64
		MaxValue    int64
	}
	selectSQL := fmt.Sprintf(
		"%s AS bucket_index, COUNT(*) AS bucket_count, MAX(%s) AS max_value",
		expression,
		column,
	)
	if err := query.Select(selectSQL).
		Group("bucket_index").
		Scan(&rows).Error; err != nil {
		return nil, 0, err
	}
	histogram := make([]uint64, len(usageLatencyBucketUpperBounds)+1)
	var maximum int64
	for _, row := range rows {
		if row.BucketIndex >= 0 && row.BucketIndex < len(histogram) {
			histogram[row.BucketIndex] = uint64(row.BucketCount)
		}
		maximum = max(maximum, row.MaxValue)
	}
	return histogram, maximum, nil
}

func usageHistogramBucketExpression(column string) (string, error) {
	if column != "duration_ms" && column != "ftut_ms" {
		return "", fmt.Errorf("unsupported histogram column")
	}
	var expression strings.Builder
	expression.WriteString("CASE")
	for index, upper := range usageLatencyBucketUpperBounds {
		fmt.Fprintf(&expression, " WHEN %s <= %d THEN %d", column, upper, index)
	}
	fmt.Fprintf(&expression, " ELSE %d END", len(usageLatencyBucketUpperBounds))
	return expression.String(), nil
}

func dbForUsageAnalytics(ctx context.Context) *gorm.DB {
	return db.GetDB().WithContext(ctx)
}
