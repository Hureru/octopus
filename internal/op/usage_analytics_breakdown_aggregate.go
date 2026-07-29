package op

import (
	"context"
	"fmt"
	"strings"

	"github.com/bestruirui/octopus/internal/model"
	"gorm.io/gorm"
)

type usageCombinedBreakdownRow struct {
	id          int
	name        string
	accumulator usageAnalyticsAccumulator
}

func usageAnalyticsBreakdownCombined(
	ctx context.Context,
	filter UsageAnalyticsFilter,
	window usageAnalyticsAggregateWindow,
	dimension string,
	config usageDimensionConfig,
	search string,
	page int,
	pageSize int,
	sortBy string,
	descending bool,
	exportAll bool,
) (UsageBreakdown, error) {
	groupColumns := config.groupColumns()
	factSelectSQL := fmt.Sprintf(`
		%s AS id,
		%s AS name,
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
		config.selectIDColumn(),
		config.nameExpression(),
	)
	factQuery := usageAnalyticsFactQuery(ctx, filter, window)
	aggregateQuery := usageAnalyticsAggregateQuery(ctx, filter, window)
	if value := strings.TrimSpace(search); value != "" {
		searchPattern := "%" + strings.ToLower(value) + "%"
		factQuery = factQuery.Where("LOWER("+config.nameExpression()+") LIKE ?", searchPattern)
		aggregateQuery = aggregateQuery.Where("LOWER("+config.nameExpression()+") LIKE ?", searchPattern)
	}
	factGroups := factQuery.Session(&gorm.Session{}).
		Select(
			factSelectSQL,
			model.RequestOutcomeSuccess,
			model.RequestOutcomeFailed,
			model.RequestOutcomeClientCanceled,
			model.RequestOutcomeIndeterminate,
			true,
			true,
		).
		Group(groupColumns)
	aggregateSelectSQL := fmt.Sprintf(`
		%s AS id,
		%s AS name,
		metric_count,
		success_count,
		failed_count,
		canceled_count,
		indeterminate_count,
		input_tokens,
		output_tokens,
		cache_read_tokens,
		cache_write_tokens,
		cost_usd,
		duration_sum_ms,
		ftut_sum_ms,
		ftut_samples`,
		config.selectIDColumn(),
		config.nameExpression(),
	)
	aggregateGroups := aggregateQuery.Session(&gorm.Session{}).Select(aggregateSelectSQL)
	parts := dbForUsageAnalytics(ctx).Raw(
		"SELECT * FROM (?) AS usage_fact_groups UNION ALL SELECT * FROM (?) AS usage_daily_groups",
		factGroups,
		aggregateGroups,
	)
	combinedBase := dbForUsageAnalytics(ctx).Table("(?) AS usage_combined_parts", parts)
	grouped := combinedBase.Session(&gorm.Session{}).
		Select("id, name").
		Group("id, name")
	var total int64
	if err := dbForUsageAnalytics(ctx).
		Table("(?) AS usage_groups", grouped).
		Count(&total).Error; err != nil {
		return UsageBreakdown{}, err
	}

	orderExpression, err := usageCombinedBreakdownOrder(sortBy)
	if err != nil {
		return UsageBreakdown{}, err
	}
	direction := " ASC"
	if descending {
		direction = " DESC"
	}
	combinedSelectSQL := `
		id AS id,
		name AS name,
		COALESCE(SUM(metric_count), 0) AS metric_count,
		COALESCE(SUM(success_count), 0) AS success_count,
		COALESCE(SUM(failed_count), 0) AS failed_count,
		COALESCE(SUM(canceled_count), 0) AS canceled_count,
		COALESCE(SUM(indeterminate_count), 0) AS indeterminate_count,
		COALESCE(SUM(input_tokens), 0) AS input_tokens,
		COALESCE(SUM(output_tokens), 0) AS output_tokens,
		COALESCE(SUM(cache_read_tokens), 0) AS cache_read_tokens,
		COALESCE(SUM(cache_write_tokens), 0) AS cache_write_tokens,
		COALESCE(SUM(cost_usd), 0) AS cost_usd,
		COALESCE(SUM(duration_sum_ms), 0) AS duration_sum_ms,
		COALESCE(SUM(ftut_sum_ms), 0) AS ftut_sum_ms,
		COALESCE(SUM(ftut_samples), 0) AS ftut_samples`
	query := combinedBase.Session(&gorm.Session{}).
		Select(combinedSelectSQL).
		Group("id, name").
		Order(orderExpression + direction).
		Order("name ASC").
		Order("id ASC")
	if !exportAll {
		query = query.Offset((page - 1) * pageSize).Limit(pageSize)
	}
	var rows []usageBreakdownScanRow
	if err := query.Scan(&rows).Error; err != nil {
		return UsageBreakdown{}, err
	}

	factAccumulators, err := usageBreakdownFactAccumulators(
		factQuery.Session(&gorm.Session{}),
		config,
		rows,
	)
	if err != nil {
		return UsageBreakdown{}, err
	}
	combined := make(map[string]*usageCombinedBreakdownRow, len(rows))
	for _, row := range rows {
		key := usageBreakdownCombinedKey(row.ID, row.Name)
		accumulator := factAccumulators[key]
		if accumulator == nil {
			accumulator = &usageAnalyticsAccumulator{}
		}
		accumulator.row = row.Metrics
		combined[key] = &usageCombinedBreakdownRow{
			id:          row.ID,
			name:        normalizeUsageAggregateName(row.Name),
			accumulator: *accumulator,
		}
	}
	if len(rows) > 0 {
		var aggregates []model.UsageAggregate
		if err := restrictUsageBreakdownQueryToItems(
			aggregateQuery.Session(&gorm.Session{}),
			config,
			rows,
		).Find(&aggregates).Error; err != nil {
			return UsageBreakdown{}, err
		}
		for i := range aggregates {
			id, name := usageAggregateDimensionValue(aggregates[i], dimension)
			if item := combined[usageBreakdownCombinedKey(id, name)]; item != nil {
				addUsageAggregateHistograms(&item.accumulator, aggregates[i])
			}
		}
	}

	items := make([]UsageBreakdownItem, 0, len(rows))
	for _, row := range rows {
		item := combined[usageBreakdownCombinedKey(row.ID, row.Name)]
		items = append(items, UsageBreakdownItem{
			ID:                    item.id,
			Name:                  item.name,
			UsageAnalyticsMetrics: item.accumulator.metrics(filter.Scope),
		})
	}
	return UsageBreakdown{
		Scope: filter.Scope, Dimension: dimension,
		Page: page, PageSize: usageBreakdownResultPageSize(pageSize, len(items), exportAll),
		Total: total, Items: items,
	}, nil
}

func addUsageAggregateHistograms(accumulator *usageAnalyticsAccumulator, item model.UsageAggregate) {
	accumulator.durationHist = mergeUsageHistograms(accumulator.durationHist, item.DurationHistogram)
	accumulator.durationMaximum = max(accumulator.durationMaximum, item.DurationMaxMS)
	accumulator.ftutHist = mergeUsageHistograms(accumulator.ftutHist, item.FTUTHistogram)
	accumulator.ftutMaximum = max(accumulator.ftutMaximum, item.FTUTMaxMS)
}

func usageAggregateDimensionValue(item model.UsageAggregate, dimension string) (int, string) {
	var id int
	var name string
	switch dimension {
	case "site":
		id, name = item.SiteID, item.SiteName
	case "site_account":
		id, name = item.SiteAccountID, item.SiteAccountName
	case "channel":
		id, name = item.ChannelID, item.ChannelName
	case "api_key":
		id, name = item.APIKeyID, item.APIKeyName
	case "request_model":
		name = item.RequestModel
	case "actual_model":
		name = item.ActualModel
	default:
		name = item.CanonicalModel
	}
	return id, normalizeUsageAggregateName(name)
}

func usageBreakdownCombinedKey(id int, name string) string {
	return fmt.Sprintf("%d\x00%s", id, strings.TrimSpace(name))
}

func usageCombinedBreakdownOrder(sortBy string) (string, error) {
	switch sortBy {
	case "", "request_count", "attempt_count":
		return "metric_count", nil
	case "total_tokens":
		return "(COALESCE(SUM(input_tokens), 0) + COALESCE(SUM(output_tokens), 0))", nil
	case "cost":
		return "cost_usd", nil
	case "success_rate":
		return "CASE WHEN (COALESCE(SUM(success_count), 0) + COALESCE(SUM(failed_count), 0)) = 0 THEN 0 ELSE " +
			"(1.0 * COALESCE(SUM(success_count), 0) / " +
			"(COALESCE(SUM(success_count), 0) + COALESCE(SUM(failed_count), 0))) END", nil
	case "duration":
		return "CASE WHEN COALESCE(SUM(metric_count), 0) = 0 THEN 0 ELSE " +
			"(1.0 * COALESCE(SUM(duration_sum_ms), 0) / COALESCE(SUM(metric_count), 0)) END", nil
	default:
		return "", fmt.Errorf("invalid sort")
	}
}
