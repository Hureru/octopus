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

type UsageTimeseriesPoint struct {
	BucketStart int64  `json:"bucket_start"`
	Label       string `json:"label"`
	UsageAnalyticsMetrics
}

type UsageTimeseries struct {
	Scope       UsageMetricScope       `json:"scope"`
	Granularity string                 `json:"granularity"`
	Timezone    string                 `json:"timezone"`
	Points      []UsageTimeseriesPoint `json:"points"`
}

type usageAnalyticsFactRow struct {
	Time             int64
	Outcome          model.RequestOutcome
	InputTokens      int64
	OutputTokens     int64
	CacheReadTokens  int64
	CacheWriteTokens int64
	CostUSD          float64
	DurationMS       int64
	FTUTMS           int64
	FTUTKnown        bool
}

type usageSeriesAccumulator struct {
	aggregate       usageAggregateRow
	durationHist    []uint64
	durationMaximum int64
	ftutHist        []uint64
	ftutMaximum     int64
}

func UsageAnalyticsTimeseriesGet(ctx context.Context, filter UsageAnalyticsFilter) (UsageTimeseries, error) {
	normalized, location, err := normalizeUsageAnalyticsFilter(filter)
	if err != nil {
		return UsageTimeseries{}, err
	}
	granularity := "day"
	if normalized.EndTime-normalized.StartTime <= int64(72*time.Hour/time.Second) {
		granularity = "hour"
	}
	window := usageAnalyticsAggregateWindow{}
	if normalized.Timezone != "UTC" &&
		normalized.StartTime < usageRetentionCutoff(time.Now(), 0) {
		normalized.Timezone = "UTC"
		location = time.UTC
	}
	if normalized.Timezone == "UTC" {
		if granularity == "hour" {
			// A narrow historical range can still be older than hourly
			// retention. Prefer the surviving daily aggregate in that case
			// instead of returning a row of empty hourly buckets.
			if dailyWindow := usageAnalyticsDailyAggregateWindow(normalized); dailyWindow.valid() {
				window = dailyWindow
				granularity = "day"
			} else {
				window = usageAnalyticsHourlyAggregateWindow(normalized)
			}
		} else {
			window = usageAnalyticsDailyAggregateWindow(normalized)
			if window.valid() {
				granularity = "day"
			}
		}
	}

	var rows []usageAnalyticsFactRow
	if err := usageAnalyticsFactQuery(ctx, normalized, window).
		Select(
			"time, outcome, input_tokens, output_tokens, cache_read_tokens, " +
				"cache_write_tokens, cost_usd, duration_ms, ftut_ms, ftut_known",
		).
		Find(&rows).Error; err != nil {
		return UsageTimeseries{}, err
	}

	accumulators := make(map[int64]*usageSeriesAccumulator)
	for _, row := range rows {
		bucketStart := usageBucketStart(row.Time, location, granularity)
		accumulator := accumulators[bucketStart]
		if accumulator == nil {
			accumulator = &usageSeriesAccumulator{}
			accumulators[bucketStart] = accumulator
		}
		addUsageFactRow(accumulator, row)
	}
	if window.valid() {
		var aggregates []model.UsageAggregate
		if err := usageAnalyticsAggregateQuery(ctx, normalized, window).Find(&aggregates).Error; err != nil {
			return UsageTimeseries{}, err
		}
		for i := range aggregates {
			accumulator := accumulators[aggregates[i].BucketStart]
			if accumulator == nil {
				accumulator = &usageSeriesAccumulator{}
				accumulators[aggregates[i].BucketStart] = accumulator
			}
			addUsageAggregateToSeries(accumulator, aggregates[i])
		}
	}

	points := make([]UsageTimeseriesPoint, 0)
	for _, bucketStart := range usageBucketRange(normalized, location, granularity) {
		metrics := UsageAnalyticsMetrics{}
		if accumulator := accumulators[bucketStart]; accumulator != nil {
			metrics = usageMetricsFromAggregate(finalizeUsageSeriesAggregate(accumulator.aggregate), normalized.Scope)
			metrics.P95DurationMS = usageHistogramPercentile95(
				accumulator.durationHist,
				accumulator.durationMaximum,
			)
			metrics.P95FTUTMS = usageHistogramPercentile95(
				accumulator.ftutHist,
				accumulator.ftutMaximum,
			)
		}
		points = append(points, UsageTimeseriesPoint{
			BucketStart:           bucketStart,
			Label:                 usageBucketLabel(bucketStart, location, granularity),
			UsageAnalyticsMetrics: metrics,
		})
	}
	return UsageTimeseries{
		Scope:       normalized.Scope,
		Granularity: granularity,
		Timezone:    normalized.Timezone,
		Points:      points,
	}, nil
}

func usageBucketStart(timestamp int64, location *time.Location, granularity string) int64 {
	local := time.Unix(timestamp, 0).In(location)
	if granularity == "hour" {
		_, offset := local.Zone()
		return ((timestamp + int64(offset)) / int64(time.Hour/time.Second) * int64(time.Hour/time.Second)) - int64(offset)
	}
	return time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, location).Unix()
}

func usageBucketRange(filter UsageAnalyticsFilter, location *time.Location, granularity string) []int64 {
	start := usageBucketStart(filter.StartTime, location, granularity)
	result := make([]int64, 0)
	current := time.Unix(start, 0).In(location)
	for current.Unix() < filter.EndTime {
		result = append(result, current.Unix())
		if granularity == "hour" {
			current = current.Add(time.Hour)
		} else {
			current = current.AddDate(0, 0, 1)
		}
	}
	return result
}

func usageBucketLabel(bucketStart int64, location *time.Location, granularity string) string {
	value := time.Unix(bucketStart, 0).In(location)
	if granularity == "hour" {
		return value.Format("2006-01-02 15:00 MST")
	}
	return value.Format("2006-01-02")
}

func addUsageFactRow(accumulator *usageSeriesAccumulator, row usageAnalyticsFactRow) {
	accumulator.aggregate.MetricCount++
	switch row.Outcome {
	case model.RequestOutcomeSuccess:
		accumulator.aggregate.SuccessCount++
	case model.RequestOutcomeFailed:
		accumulator.aggregate.FailedCount++
	case model.RequestOutcomeClientCanceled:
		accumulator.aggregate.CanceledCount++
	default:
		accumulator.aggregate.IndeterminateCount++
	}
	accumulator.aggregate.InputTokens += row.InputTokens
	accumulator.aggregate.OutputTokens += row.OutputTokens
	accumulator.aggregate.CacheReadTokens += row.CacheReadTokens
	accumulator.aggregate.CacheWriteTokens += row.CacheWriteTokens
	accumulator.aggregate.CostUSD += row.CostUSD
	accumulator.aggregate.DurationSumMS += row.DurationMS
	if accumulator.durationHist == nil {
		accumulator.durationHist = make([]uint64, len(usageLatencyBucketUpperBounds)+1)
	}
	addUsageHistogramValue(accumulator.durationHist, row.DurationMS)
	accumulator.durationMaximum = max(accumulator.durationMaximum, row.DurationMS)
	if row.FTUTKnown {
		accumulator.aggregate.FTUTSumMS += row.FTUTMS
		accumulator.aggregate.FTUTSamples++
		if accumulator.ftutHist == nil {
			accumulator.ftutHist = make([]uint64, len(usageLatencyBucketUpperBounds)+1)
		}
		addUsageHistogramValue(accumulator.ftutHist, row.FTUTMS)
		accumulator.ftutMaximum = max(accumulator.ftutMaximum, row.FTUTMS)
	}
}

func addUsageAggregateToSeries(accumulator *usageSeriesAccumulator, row model.UsageAggregate) {
	accumulator.aggregate.MetricCount += row.MetricCount
	accumulator.aggregate.SuccessCount += row.SuccessCount
	accumulator.aggregate.FailedCount += row.FailedCount
	accumulator.aggregate.CanceledCount += row.CanceledCount
	accumulator.aggregate.IndeterminateCount += row.IndeterminateCount
	accumulator.aggregate.InputTokens += row.InputTokens
	accumulator.aggregate.OutputTokens += row.OutputTokens
	accumulator.aggregate.CacheReadTokens += row.CacheReadTokens
	accumulator.aggregate.CacheWriteTokens += row.CacheWriteTokens
	accumulator.aggregate.CostUSD += row.CostUSD
	accumulator.aggregate.DurationSumMS += row.DurationSumMS
	accumulator.aggregate.FTUTSumMS += row.FTUTSumMS
	accumulator.aggregate.FTUTSamples += row.FTUTSamples
	accumulator.durationHist = mergeUsageHistograms(accumulator.durationHist, row.DurationHistogram)
	accumulator.durationMaximum = max(accumulator.durationMaximum, row.DurationMaxMS)
	accumulator.ftutHist = mergeUsageHistograms(accumulator.ftutHist, row.FTUTHistogram)
	accumulator.ftutMaximum = max(accumulator.ftutMaximum, row.FTUTMaxMS)
}

func finalizeUsageSeriesAggregate(row usageAggregateRow) usageAggregateRow {
	return row
}

type UsageBreakdownItem struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
	UsageAnalyticsMetrics
}

type UsageBreakdown struct {
	Scope     UsageMetricScope     `json:"scope"`
	Dimension string               `json:"dimension"`
	Page      int                  `json:"page"`
	PageSize  int                  `json:"page_size"`
	Total     int64                `json:"total"`
	Items     []UsageBreakdownItem `json:"items"`
}

type usageBreakdownScanRow struct {
	ID      int
	Name    string
	Metrics usageAggregateRow `gorm:"embedded"`
}

type usageBreakdownHistogramRow struct {
	ID          int
	Name        string
	BucketIndex int
	BucketCount int64
	MaxValue    int64
}

type usageDimensionConfig struct {
	idColumn   string
	nameColumn string
}

var usageDimensions = map[string]usageDimensionConfig{
	"site":            {idColumn: "site_id", nameColumn: "site_name"},
	"site_account":    {idColumn: "site_account_id", nameColumn: "site_account_name"},
	"channel":         {idColumn: "channel_id", nameColumn: "channel_name"},
	"api_key":         {idColumn: "api_key_id", nameColumn: "api_key_name"},
	"request_model":   {nameColumn: "request_model"},
	"actual_model":    {nameColumn: "actual_model"},
	"canonical_model": {nameColumn: "canonical_model"},
}

const (
	usageBreakdownMaxPageSize    = 50
	usageBreakdownExportPageSize = 250
)

func (config usageDimensionConfig) groupColumns() string {
	if config.idColumn == "" {
		return config.nameExpression()
	}
	return config.idExpression() + ", " + config.nameExpression()
}

func (config usageDimensionConfig) selectIDColumn() string {
	return config.idExpression()
}

func (config usageDimensionConfig) idExpression() string {
	if config.idColumn == "" {
		return "0"
	}
	return "COALESCE(" + config.idColumn + ", 0)"
}

func (config usageDimensionConfig) nameExpression() string {
	normalized := "TRIM(REPLACE(REPLACE(REPLACE(COALESCE(" + config.nameColumn +
		", ''), '\t', ' '), '\n', ' '), '\r', ' '))"
	return "CASE WHEN " + normalized + " = '' THEN 'Unmanaged' ELSE " + normalized + " END"
}

func UsageAnalyticsBreakdownGet(
	ctx context.Context,
	filter UsageAnalyticsFilter,
	dimension string,
	page int,
	pageSize int,
	sortBy string,
	descending bool,
) (UsageBreakdown, error) {
	return UsageAnalyticsBreakdownSearchGet(
		ctx,
		filter,
		dimension,
		"",
		page,
		pageSize,
		sortBy,
		descending,
	)
}

func UsageAnalyticsBreakdownSearchGet(
	ctx context.Context,
	filter UsageAnalyticsFilter,
	dimension string,
	search string,
	page int,
	pageSize int,
	sortBy string,
	descending bool,
) (UsageBreakdown, error) {
	return usageAnalyticsBreakdownSearchGet(
		ctx,
		filter,
		dimension,
		search,
		page,
		pageSize,
		sortBy,
		descending,
		false,
		usageBreakdownMaxPageSize,
	)
}

func UsageAnalyticsBreakdownExportGet(
	ctx context.Context,
	filter UsageAnalyticsFilter,
	dimension string,
	search string,
	sortBy string,
	descending bool,
) (UsageBreakdown, error) {
	return usageAnalyticsBreakdownSearchGet(
		ctx,
		filter,
		dimension,
		search,
		1,
		0,
		sortBy,
		descending,
		true,
		usageBreakdownMaxPageSize,
	)
}

func UsageAnalyticsBreakdownExportPageGet(
	ctx context.Context,
	filter UsageAnalyticsFilter,
	dimension string,
	search string,
	page int,
	sortBy string,
	descending bool,
) (UsageBreakdown, error) {
	return usageAnalyticsBreakdownSearchGet(
		ctx,
		filter,
		dimension,
		search,
		page,
		usageBreakdownExportPageSize,
		sortBy,
		descending,
		false,
		usageBreakdownExportPageSize,
	)
}

func usageAnalyticsBreakdownSearchGet(
	ctx context.Context,
	filter UsageAnalyticsFilter,
	dimension string,
	search string,
	page int,
	pageSize int,
	sortBy string,
	descending bool,
	exportAll bool,
	maxPageSize int,
) (UsageBreakdown, error) {
	normalized, _, err := normalizeUsageAnalyticsFilter(filter)
	if err != nil {
		return UsageBreakdown{}, err
	}
	config, ok := usageDimensions[dimension]
	if !ok {
		return UsageBreakdown{}, fmt.Errorf("invalid dimension")
	}
	if page < 1 {
		page = 1
	}
	if exportAll {
		page = 1
		pageSize = 0
	} else {
		if pageSize <= 0 {
			pageSize = 20
		}
		if maxPageSize <= 0 {
			maxPageSize = usageBreakdownMaxPageSize
		}
		if pageSize > maxPageSize {
			pageSize = maxPageSize
		}
	}
	window := usageAnalyticsDailyAggregateWindow(normalized)
	if window.valid() {
		return usageAnalyticsBreakdownCombined(
			ctx,
			normalized,
			window,
			dimension,
			config,
			search,
			page,
			pageSize,
			sortBy,
			descending,
			exportAll,
		)
	}

	groupColumns := config.groupColumns()
	base := usageAnalyticsBaseQuery(ctx, normalized)
	if value := strings.TrimSpace(search); value != "" {
		base = base.Where("LOWER("+config.nameExpression()+") LIKE ?", "%"+strings.ToLower(value)+"%")
	}
	subquery := base.Session(&gorm.Session{}).Select(groupColumns).Group(groupColumns)
	var total int64
	if err := db.GetDB().WithContext(ctx).Table("(?) AS usage_groups", subquery).Count(&total).Error; err != nil {
		return UsageBreakdown{}, err
	}

	orderExpression, err := usageBreakdownOrder(sortBy)
	if err != nil {
		return UsageBreakdown{}, err
	}
	direction := " ASC"
	if descending {
		direction = " DESC"
	}
	var rows []usageBreakdownScanRow
	selectSQL := fmt.Sprintf(`
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
	query := base.Session(&gorm.Session{}).Select(
		selectSQL,
		model.RequestOutcomeSuccess,
		model.RequestOutcomeFailed,
		model.RequestOutcomeClientCanceled,
		model.RequestOutcomeIndeterminate,
		true,
		true,
	).
		Group(groupColumns).
		Order(orderExpression + direction).
		Order("name ASC").
		Order("id ASC")
	if !exportAll {
		query = query.Offset((page - 1) * pageSize).Limit(pageSize)
	}
	if err := query.Scan(&rows).Error; err != nil {
		return UsageBreakdown{}, err
	}
	accumulators, err := usageBreakdownFactAccumulators(
		base.Session(&gorm.Session{}),
		config,
		rows,
	)
	if err != nil {
		return UsageBreakdown{}, err
	}

	items := make([]UsageBreakdownItem, 0, len(rows))
	for _, row := range rows {
		name := strings.TrimSpace(row.Name)
		if name == "" {
			name = "Unmanaged"
		}
		accumulator := accumulators[usageBreakdownCombinedKey(row.ID, row.Name)]
		if accumulator == nil {
			accumulator = &usageAnalyticsAccumulator{}
		}
		accumulator.row = row.Metrics
		items = append(items, UsageBreakdownItem{
			ID:                    row.ID,
			Name:                  name,
			UsageAnalyticsMetrics: accumulator.metrics(normalized.Scope),
		})
	}
	return UsageBreakdown{
		Scope:     normalized.Scope,
		Dimension: dimension,
		Page:      page,
		PageSize:  usageBreakdownResultPageSize(pageSize, len(items), exportAll),
		Total:     total,
		Items:     items,
	}, nil
}

func usageBreakdownFactAccumulators(
	query *gorm.DB,
	config usageDimensionConfig,
	rows []usageBreakdownScanRow,
) (map[string]*usageAnalyticsAccumulator, error) {
	accumulators := make(map[string]*usageAnalyticsAccumulator, len(rows))
	if len(rows) == 0 {
		return accumulators, nil
	}
	if err := addUsageBreakdownHistogram(
		query.Session(&gorm.Session{}),
		config,
		rows,
		"duration_ms",
		false,
		accumulators,
	); err != nil {
		return nil, err
	}
	if err := addUsageBreakdownHistogram(
		query.Session(&gorm.Session{}),
		config,
		rows,
		"ftut_ms",
		true,
		accumulators,
	); err != nil {
		return nil, err
	}
	return accumulators, nil
}

func addUsageBreakdownHistogram(
	query *gorm.DB,
	config usageDimensionConfig,
	items []usageBreakdownScanRow,
	column string,
	requireFTUT bool,
	accumulators map[string]*usageAnalyticsAccumulator,
) error {
	expression, err := usageHistogramBucketExpression(column)
	if err != nil {
		return err
	}
	if requireFTUT {
		query = query.Where("ftut_known = ?", true)
	}
	selectSQL := fmt.Sprintf(
		"%s AS id, %s AS name, %s AS bucket_index, COUNT(*) AS bucket_count, MAX(%s) AS max_value",
		config.selectIDColumn(),
		config.nameExpression(),
		expression,
		column,
	)

	const identityBatchSize = 200
	for start := 0; start < len(items); start += identityBatchSize {
		end := min(start+identityBatchSize, len(items))
		batchQuery := restrictUsageBreakdownQueryToItems(
			query.Session(&gorm.Session{}),
			config,
			items[start:end],
		)
		var rows []usageBreakdownHistogramRow
		if err := batchQuery.
			Select(selectSQL).
			Group(config.groupColumns()).
			Group("bucket_index").
			Scan(&rows).Error; err != nil {
			return err
		}
		for _, row := range rows {
			if row.BucketIndex < 0 || row.BucketIndex > len(usageLatencyBucketUpperBounds) {
				continue
			}
			key := usageBreakdownCombinedKey(row.ID, row.Name)
			accumulator := accumulators[key]
			if accumulator == nil {
				accumulator = &usageAnalyticsAccumulator{}
				accumulators[key] = accumulator
			}
			histogram := &accumulator.durationHist
			maximum := &accumulator.durationMaximum
			if requireFTUT {
				histogram = &accumulator.ftutHist
				maximum = &accumulator.ftutMaximum
			}
			if *histogram == nil {
				*histogram = make([]uint64, len(usageLatencyBucketUpperBounds)+1)
			}
			(*histogram)[row.BucketIndex] += uint64(row.BucketCount)
			*maximum = max(*maximum, row.MaxValue)
		}
	}
	return nil
}

func restrictUsageBreakdownQueryToItems(
	query *gorm.DB,
	config usageDimensionConfig,
	items []usageBreakdownScanRow,
) *gorm.DB {
	if config.idColumn != "" {
		ids := make([]int, 0, len(items))
		seen := make(map[int]struct{}, len(items))
		for _, item := range items {
			if _, ok := seen[item.ID]; ok {
				continue
			}
			seen[item.ID] = struct{}{}
			ids = append(ids, item.ID)
		}
		return query.Where(config.idExpression()+" IN ?", ids)
	}
	names := make([]string, 0, len(items))
	seen := make(map[string]struct{}, len(items))
	for _, item := range items {
		if _, ok := seen[item.Name]; ok {
			continue
		}
		seen[item.Name] = struct{}{}
		names = append(names, item.Name)
	}
	return query.Where(config.nameExpression()+" IN ?", names)
}

func usageBreakdownResultPageSize(pageSize, itemCount int, exportAll bool) int {
	if exportAll {
		return itemCount
	}
	return pageSize
}

func usageBreakdownOrder(sortBy string) (string, error) {
	switch sortBy {
	case "", "request_count", "attempt_count":
		return "metric_count", nil
	case "total_tokens":
		return "(COALESCE(SUM(input_tokens), 0) + COALESCE(SUM(output_tokens), 0))", nil
	case "cost":
		return "cost_usd", nil
	case "success_rate":
		return "CASE WHEN SUM(CASE WHEN outcome IN ('success','failed') THEN 1 ELSE 0 END) = 0 THEN 0 ELSE " +
			"(1.0 * SUM(CASE WHEN outcome = 'success' THEN 1 ELSE 0 END) / " +
			"SUM(CASE WHEN outcome IN ('success','failed') THEN 1 ELSE 0 END)) END", nil
	case "duration":
		return "CASE WHEN COUNT(*) = 0 THEN 0 ELSE " +
			"(1.0 * COALESCE(SUM(duration_ms), 0) / COUNT(*)) END", nil
	default:
		return "", fmt.Errorf("invalid sort")
	}
}

type UsageDimensionOption struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
}

type UsageDimensionsResult struct {
	Dimension string                 `json:"dimension"`
	Page      int                    `json:"page"`
	PageSize  int                    `json:"page_size"`
	HasMore   bool                   `json:"has_more"`
	Items     []UsageDimensionOption `json:"items"`
}

func UsageAnalyticsDimensionsGet(
	ctx context.Context,
	filter UsageAnalyticsFilter,
	dimension string,
	search string,
	page int,
	pageSize int,
) (UsageDimensionsResult, error) {
	normalized, _, err := normalizeUsageAnalyticsFilter(filter)
	if err != nil {
		return UsageDimensionsResult{}, err
	}
	config, ok := usageDimensions[dimension]
	if !ok {
		return UsageDimensionsResult{}, fmt.Errorf("invalid dimension")
	}
	if page < 1 {
		page = 1
	}
	if pageSize <= 0 {
		pageSize = 50
	}
	if pageSize > 100 {
		pageSize = 100
	}
	query := usageAnalyticsBaseQuery(ctx, normalized)
	if value := strings.TrimSpace(search); value != "" {
		query = query.Where("LOWER("+config.nameExpression()+") LIKE ?", "%"+strings.ToLower(value)+"%")
	}
	var rows []UsageDimensionOption
	selectSQL := config.selectIDColumn() + " AS id, " + config.nameExpression() + " AS name"
	if err := query.
		Select(selectSQL).
		Group(config.groupColumns()).
		Order("name ASC").
		Offset((page - 1) * pageSize).
		Limit(pageSize + 1).
		Scan(&rows).Error; err != nil {
		return UsageDimensionsResult{}, err
	}
	hasMore := len(rows) > pageSize
	if hasMore {
		rows = rows[:pageSize]
	}
	for i := range rows {
		if strings.TrimSpace(rows[i].Name) == "" {
			rows[i].Name = "Unmanaged"
		}
	}
	return UsageDimensionsResult{
		Dimension: dimension,
		Page:      page,
		PageSize:  pageSize,
		HasMore:   hasMore,
		Items:     rows,
	}, nil
}
