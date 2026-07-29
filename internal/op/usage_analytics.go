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

type UsageMetricScope string

const (
	UsageMetricScopeRequest UsageMetricScope = "request"
	UsageMetricScopeAttempt UsageMetricScope = "attempt"
)

type UsageAnalyticsFilter struct {
	StartTime       int64
	EndTime         int64
	Timezone        string
	Scope           UsageMetricScope
	SiteIDs         []int
	SiteAccountIDs  []int
	ChannelIDs      []int
	APIKeyIDs       []int
	RequestModels   []string
	ActualModels    []string
	CanonicalModels []string
}

type UsageAnalyticsMetrics struct {
	MetricCount        int64   `json:"metric_count"`
	RequestCount       int64   `json:"request_count"`
	AttemptCount       int64   `json:"attempt_count"`
	SuccessCount       int64   `json:"success_count"`
	FailedCount        int64   `json:"failed_count"`
	CanceledCount      int64   `json:"canceled_count"`
	IndeterminateCount int64   `json:"indeterminate_count"`
	SuccessRate        float64 `json:"success_rate"`
	InputTokens        int64   `json:"input_tokens"`
	OutputTokens       int64   `json:"output_tokens"`
	CacheReadTokens    int64   `json:"cache_read_tokens"`
	CacheWriteTokens   int64   `json:"cache_write_tokens"`
	TotalTokens        int64   `json:"total_tokens"`
	CostUSD            float64 `json:"cost_usd"`
	AverageDurationMS  float64 `json:"average_duration_ms"`
	P95DurationMS      int64   `json:"p95_duration_ms"`
	AverageFTUTMS      float64 `json:"average_ftut_ms"`
	P95FTUTMS          int64   `json:"p95_ftut_ms"`
	FTUTSamples        int64   `json:"ftut_samples"`
}

type UsageAnalyticsSummary struct {
	UsageAnalyticsMetrics
	Scope                UsageMetricScope `json:"scope"`
	StartTime            int64            `json:"start_time"`
	EndTime              int64            `json:"end_time"`
	Timezone             string           `json:"timezone"`
	DrilldownAvailable   bool             `json:"drilldown_available"`
	EarliestRelayLogTime *int64           `json:"earliest_relay_log_time,omitempty"`
}

type usageAggregateRow struct {
	MetricCount        int64
	SuccessCount       int64
	FailedCount        int64
	CanceledCount      int64
	IndeterminateCount int64
	InputTokens        int64
	OutputTokens       int64
	CacheReadTokens    int64
	CacheWriteTokens   int64
	CostUSD            float64
	DurationSumMS      int64
	FTUTSumMS          int64
	FTUTSamples        int64
}

func UsageAnalyticsSummaryGet(ctx context.Context, filter UsageAnalyticsFilter) (UsageAnalyticsSummary, error) {
	normalized, _, err := normalizeUsageAnalyticsFilter(filter)
	if err != nil {
		return UsageAnalyticsSummary{}, err
	}
	accumulator, err := usageAnalyticsSummaryAccumulator(ctx, normalized)
	if err != nil {
		return UsageAnalyticsSummary{}, err
	}

	metrics := accumulator.metrics(normalized.Scope)
	earliest, available, err := usageDrilldownAvailability(
		ctx,
		normalized.StartTime,
		normalized.EndTime,
	)
	if err != nil {
		return UsageAnalyticsSummary{}, err
	}
	return UsageAnalyticsSummary{
		UsageAnalyticsMetrics: metrics,
		Scope:                 normalized.Scope,
		StartTime:             normalized.StartTime,
		EndTime:               normalized.EndTime,
		Timezone:              normalized.Timezone,
		DrilldownAvailable:    available,
		EarliestRelayLogTime:  earliest,
	}, nil
}

func normalizeUsageAnalyticsFilter(filter UsageAnalyticsFilter) (UsageAnalyticsFilter, *time.Location, error) {
	if filter.Scope == "" {
		filter.Scope = UsageMetricScopeRequest
	}
	if filter.Scope != UsageMetricScopeRequest && filter.Scope != UsageMetricScopeAttempt {
		return filter, nil, fmt.Errorf("invalid metric scope")
	}
	if strings.TrimSpace(filter.Timezone) == "" {
		filter.Timezone = "UTC"
	}
	location, err := time.LoadLocation(filter.Timezone)
	if err != nil {
		return filter, nil, fmt.Errorf("invalid timezone: %w", err)
	}
	now := time.Now()
	if filter.EndTime <= 0 {
		filter.EndTime = now.Unix()
	}
	if filter.StartTime <= 0 {
		localNow := now.In(location)
		filter.StartTime = time.Date(
			localNow.Year(),
			localNow.Month(),
			localNow.Day(),
			0,
			0,
			0,
			0,
			location,
		).Unix()
	}
	if filter.EndTime <= filter.StartTime {
		return filter, nil, fmt.Errorf("end_time must be greater than start_time")
	}
	return filter, location, nil
}

func usageAnalyticsBaseQuery(ctx context.Context, filter UsageAnalyticsFilter) *gorm.DB {
	var query *gorm.DB
	if filter.Scope == UsageMetricScopeAttempt {
		query = db.GetDB().WithContext(ctx).Model(&model.UsageAttemptFact{})
	} else {
		query = db.GetDB().WithContext(ctx).Model(&model.UsageRequestFact{})
	}
	query = query.Where("time >= ? AND time < ?", filter.StartTime, filter.EndTime)
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

func usageMetricsFromAggregate(row usageAggregateRow, scope UsageMetricScope) UsageAnalyticsMetrics {
	denominator := row.SuccessCount + row.FailedCount
	successRate := float64(0)
	if denominator > 0 {
		successRate = float64(row.SuccessCount) / float64(denominator)
	}
	metrics := UsageAnalyticsMetrics{
		MetricCount:        row.MetricCount,
		SuccessCount:       row.SuccessCount,
		FailedCount:        row.FailedCount,
		CanceledCount:      row.CanceledCount,
		IndeterminateCount: row.IndeterminateCount,
		SuccessRate:        successRate,
		InputTokens:        row.InputTokens,
		OutputTokens:       row.OutputTokens,
		CacheReadTokens:    row.CacheReadTokens,
		CacheWriteTokens:   row.CacheWriteTokens,
		TotalTokens:        row.InputTokens + row.OutputTokens,
		CostUSD:            row.CostUSD,
		FTUTSamples:        row.FTUTSamples,
	}
	if row.MetricCount > 0 {
		metrics.AverageDurationMS = float64(row.DurationSumMS) / float64(row.MetricCount)
	}
	if row.FTUTSamples > 0 {
		metrics.AverageFTUTMS = float64(row.FTUTSumMS) / float64(row.FTUTSamples)
	}
	if scope == UsageMetricScopeAttempt {
		metrics.AttemptCount = row.MetricCount
	} else {
		metrics.RequestCount = row.MetricCount
	}
	return metrics
}

func usageDrilldownAvailability(
	ctx context.Context,
	startTime int64,
	endTime int64,
) (*int64, bool, error) {
	var earliest *int64
	if err := db.GetDB().WithContext(ctx).
		Model(&model.RelayLog{}).
		Select("MIN(time)").
		Scan(&earliest).Error; err != nil {
		return nil, false, err
	}
	if earliest == nil {
		return nil, false, nil
	}
	var count int64
	if err := db.GetDB().WithContext(ctx).
		Model(&model.RelayLog{}).
		Where("time >= ? AND time < ?", startTime, endTime).
		Limit(1).
		Count(&count).Error; err != nil {
		return nil, false, err
	}
	return earliest, count > 0, nil
}
