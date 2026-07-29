package op

import (
	"context"
	"fmt"
	"math"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	dbpkg "github.com/bestruirui/octopus/internal/db"
	"github.com/bestruirui/octopus/internal/model"
	"github.com/bestruirui/octopus/internal/transformer/outbound"
)

func TestUsageAnalyticsSeparatesRequestsAndAttempts(t *testing.T) {
	ctx := setupSiteOpTestDB(t)
	resetRelayLogStateForTest()
	fixture := createUsageAnalyticsFixture(t, ctx)

	logs := []model.RelayLog{
		{
			ID:                 9001,
			Time:               1_720_000_000,
			RequestAPIKeyID:    10,
			RequestAPIKeyName:  "terminal",
			RequestModelName:   "gpt",
			ActualModelName:    "gpt-upstream",
			CanonicalModelName: "gpt",
			ChannelId:          fixture.channelB.ID,
			ChannelName:        fixture.channelB.Name,
			Outcome:            model.RequestOutcomeSuccess,
			Success:            true,
			InputTokens:        100,
			OutputTokens:       40,
			CacheReadTokens:    intPtr(20),
			Cost:               0.25,
			UseTime:            800,
			Ftut:               120,
			TokenSource:        model.UsageValueSourceReported,
			Attempts: []model.ChannelAttempt{
				{
					ChannelID:   fixture.channelA.ID,
					ChannelName: fixture.channelA.Name,
					ModelName:   "gpt-upstream",
					AttemptNum:  1,
					Status:      model.AttemptFailed,
					Outcome:     model.RequestOutcomeFailed,
					Attribution: model.AttemptAttributionUpstream,
					Duration:    200,
				},
				{
					ChannelID:   fixture.channelB.ID,
					ChannelName: fixture.channelB.Name,
					ModelName:   "gpt-upstream",
					AttemptNum:  2,
					Status:      model.AttemptSuccess,
					Outcome:     model.RequestOutcomeSuccess,
					Duration:    550,
				},
			},
		},
		{
			ID:                 9002,
			Time:               1_720_000_100,
			RequestAPIKeyID:    10,
			RequestAPIKeyName:  "terminal",
			RequestModelName:   "gpt",
			ActualModelName:    "gpt-upstream",
			CanonicalModelName: "gpt",
			ChannelId:          fixture.channelB.ID,
			ChannelName:        fixture.channelB.Name,
			Outcome:            model.RequestOutcomeClientCanceled,
			InputTokens:        30,
			OutputTokens:       5,
			Cost:               0.05,
			UseTime:            300,
			TokenSource:        model.UsageValueSourceReported,
			Attempts: []model.ChannelAttempt{
				{
					ChannelID:   fixture.channelB.ID,
					ChannelName: fixture.channelB.Name,
					ModelName:   "gpt-upstream",
					AttemptNum:  1,
					Status:      model.AttemptCanceled,
					Outcome:     model.RequestOutcomeClientCanceled,
					Attribution: model.AttemptAttributionClient,
					Duration:    250,
				},
			},
		},
	}
	for _, relayLog := range logs {
		if err := RelayLogAdd(ctx, relayLog); err != nil {
			t.Fatalf("RelayLogAdd failed: %v", err)
		}
	}
	if err := RelayLogFlushPending(ctx); err != nil {
		t.Fatalf("RelayLogFlushPending failed: %v", err)
	}

	filter := UsageAnalyticsFilter{
		StartTime: 1_719_999_000,
		EndTime:   1_720_001_000,
		Timezone:  "Asia/Shanghai",
		Scope:     UsageMetricScopeRequest,
	}
	requestSummary, err := UsageAnalyticsSummaryGet(ctx, filter)
	if err != nil {
		t.Fatalf("UsageAnalyticsSummaryGet request failed: %v", err)
	}
	if requestSummary.RequestCount != 2 ||
		requestSummary.AttemptCount != 0 ||
		requestSummary.SuccessCount != 1 ||
		requestSummary.FailedCount != 0 ||
		requestSummary.CanceledCount != 1 {
		t.Fatalf("unexpected request summary: %+v", requestSummary)
	}
	if requestSummary.TotalTokens != 175 || math.Abs(requestSummary.CostUSD-0.30) > 1e-9 {
		t.Fatalf("unexpected request usage totals: %+v", requestSummary)
	}

	filter.Scope = UsageMetricScopeAttempt
	attemptSummary, err := UsageAnalyticsSummaryGet(ctx, filter)
	if err != nil {
		t.Fatalf("UsageAnalyticsSummaryGet attempt failed: %v", err)
	}
	if attemptSummary.AttemptCount != 3 ||
		attemptSummary.SuccessCount != 1 ||
		attemptSummary.FailedCount != 1 ||
		attemptSummary.CanceledCount != 1 {
		t.Fatalf("unexpected attempt summary: %+v", attemptSummary)
	}
	if math.Abs(attemptSummary.SuccessRate-0.5) > 1e-9 {
		t.Fatalf("client cancellation changed supplier success rate: %+v", attemptSummary)
	}

	breakdown, err := UsageAnalyticsBreakdownGet(ctx, filter, "site", 1, 20, "request_count", true)
	if err != nil {
		t.Fatalf("UsageAnalyticsBreakdownGet failed: %v", err)
	}
	if breakdown.Total != 2 || len(breakdown.Items) != 2 {
		t.Fatalf("unexpected site breakdown: %+v", breakdown)
	}
	searchedBreakdown, err := UsageAnalyticsBreakdownSearchGet(
		ctx,
		filter,
		"site",
		"site a",
		1,
		20,
		"request_count",
		true,
	)
	if err != nil {
		t.Fatalf("searched site breakdown failed: %v", err)
	}
	if searchedBreakdown.Total != 1 ||
		len(searchedBreakdown.Items) != 1 ||
		searchedBreakdown.Items[0].Name != "Site A" {
		t.Fatalf("unexpected searched breakdown: %+v", searchedBreakdown)
	}
	if searchedBreakdown.Items[0].AttemptCount != 1 ||
		searchedBreakdown.Items[0].FailedCount != 1 {
		t.Fatalf("searched breakdown lost fact metrics: %+v", searchedBreakdown)
	}

	series, err := UsageAnalyticsTimeseriesGet(ctx, filter)
	if err != nil {
		t.Fatalf("UsageAnalyticsTimeseriesGet failed: %v", err)
	}
	var attemptCount int64
	for _, point := range series.Points {
		attemptCount += point.AttemptCount
	}
	if attemptCount != 3 {
		t.Fatalf("unexpected timeseries attempt count: %+v", series)
	}

	dimensions, err := UsageAnalyticsDimensionsGet(ctx, filter, "site", "", 1, 50)
	if err != nil {
		t.Fatalf("UsageAnalyticsDimensionsGet failed: %v", err)
	}
	if len(dimensions.Items) != 2 {
		t.Fatalf("unexpected site dimensions: %+v", dimensions)
	}
	for _, dimension := range []string{"request_model", "actual_model", "canonical_model"} {
		dimensions, err = UsageAnalyticsDimensionsGet(ctx, filter, dimension, "", 1, 50)
		if err != nil {
			t.Fatalf("UsageAnalyticsDimensionsGet(%s) failed: %v", dimension, err)
		}
		if len(dimensions.Items) != 1 || dimensions.Items[0].ID != 0 {
			t.Fatalf("unexpected %s dimensions: %+v", dimension, dimensions)
		}
	}
	durationBreakdown, err := UsageAnalyticsBreakdownGet(
		ctx,
		filter,
		"channel",
		1,
		20,
		"duration",
		true,
	)
	if err != nil {
		t.Fatalf("duration breakdown failed: %v", err)
	}
	if len(durationBreakdown.Items) != 2 ||
		durationBreakdown.Items[0].AverageDurationMS < durationBreakdown.Items[1].AverageDurationMS {
		t.Fatalf("duration breakdown is not descending: %+v", durationBreakdown)
	}
}

func TestUsageBreakdownScansCurrentFactMetrics(t *testing.T) {
	ctx := setupSiteOpTestDB(t)
	now := time.Now().Unix()
	fact := model.UsageRequestFact{
		RelayLogID:      9050,
		Time:            now - 60,
		APIKeyID:        42,
		APIKeyName:      "E2E Terminal",
		Outcome:         model.RequestOutcomeSuccess,
		InputTokens:     3,
		OutputTokens:    2,
		CacheReadTokens: 1,
		CostUSD:         0.012,
		DurationMS:      240,
		FTUTMS:          80,
		FTUTKnown:       true,
	}
	if err := dbpkg.GetDB().WithContext(ctx).Create(&fact).Error; err != nil {
		t.Fatalf("create usage fact: %v", err)
	}

	breakdown, err := UsageAnalyticsBreakdownSearchGet(
		ctx,
		UsageAnalyticsFilter{
			StartTime: now - 300,
			EndTime:   now + 1,
			Timezone:  "UTC",
			Scope:     UsageMetricScopeRequest,
		},
		"api_key",
		"E2E",
		1,
		20,
		"request_count",
		true,
	)
	if err != nil {
		t.Fatalf("usage fact breakdown: %v", err)
	}
	if breakdown.Total != 1 || len(breakdown.Items) != 1 {
		t.Fatalf("unexpected usage fact breakdown: %+v", breakdown)
	}
	item := breakdown.Items[0]
	if item.Name != "E2E Terminal" ||
		item.MetricCount != 1 ||
		item.RequestCount != 1 ||
		item.SuccessCount != 1 ||
		item.InputTokens != 3 ||
		item.OutputTokens != 2 ||
		item.CacheReadTokens != 1 ||
		item.TotalTokens != 5 ||
		math.Abs(item.CostUSD-0.012) > 1e-9 ||
		math.Abs(item.AverageDurationMS-240) > 1e-9 ||
		item.P95DurationMS != 250 ||
		math.Abs(item.AverageFTUTMS-80) > 1e-9 ||
		item.P95FTUTMS != 100 {
		t.Fatalf("usage fact breakdown lost metrics: %+v", item)
	}
}

func TestUsageBreakdownNormalizesUnmanagedDimensionNames(t *testing.T) {
	ctx := setupSiteOpTestDB(t)
	now := time.Now().Unix()
	facts := []model.UsageRequestFact{
		{
			RelayLogID: 9051,
			Time:       now - 60,
			SiteID:     0,
			SiteName:   "",
			Outcome:    model.RequestOutcomeSuccess,
		},
		{
			RelayLogID: 9052,
			Time:       now - 60,
			SiteID:     0,
			SiteName:   " \t",
			Outcome:    model.RequestOutcomeFailed,
		},
	}
	if err := dbpkg.GetDB().WithContext(ctx).Create(&facts).Error; err != nil {
		t.Fatalf("create unmanaged usage facts: %v", err)
	}
	if err := dbpkg.GetDB().WithContext(ctx).
		Model(&model.UsageRequestFact{}).
		Where("relay_log_id = ?", facts[0].RelayLogID).
		UpdateColumn("site_id", nil).Error; err != nil {
		t.Fatalf("make legacy unmanaged site id nullable: %v", err)
	}
	filter := UsageAnalyticsFilter{
		StartTime: now - 300,
		EndTime:   now + 1,
		Timezone:  "UTC",
		Scope:     UsageMetricScopeRequest,
	}
	breakdown, err := UsageAnalyticsBreakdownSearchGet(
		ctx,
		filter,
		"site",
		"unmanaged",
		1,
		20,
		"request_count",
		true,
	)
	if err != nil {
		t.Fatalf("unmanaged breakdown: %v", err)
	}
	if breakdown.Total != 1 ||
		len(breakdown.Items) != 1 ||
		breakdown.Items[0].Name != "Unmanaged" ||
		breakdown.Items[0].RequestCount != 2 {
		t.Fatalf("unmanaged names were not grouped consistently: %+v", breakdown)
	}

	dimensions, err := UsageAnalyticsDimensionsGet(
		ctx,
		filter,
		"site",
		"unmanaged",
		1,
		20,
	)
	if err != nil {
		t.Fatalf("unmanaged dimensions: %v", err)
	}
	if len(dimensions.Items) != 1 ||
		dimensions.Items[0].ID != 0 ||
		dimensions.Items[0].Name != "Unmanaged" {
		t.Fatalf("unmanaged dimension options were duplicated: %+v", dimensions)
	}
}

func TestUsageBreakdownPaginationIsStableAndMatchesExport(t *testing.T) {
	ctx := setupSiteOpTestDB(t)
	now := time.Now().Unix()
	facts := []model.UsageRequestFact{
		{RelayLogID: 9061, Time: now - 60, APIKeyID: 1, APIKeyName: "Alpha", Outcome: model.RequestOutcomeSuccess},
		{RelayLogID: 9062, Time: now - 60, APIKeyID: 2, APIKeyName: "Alpha", Outcome: model.RequestOutcomeSuccess},
		{RelayLogID: 9063, Time: now - 60, APIKeyID: 3, APIKeyName: "Beta", Outcome: model.RequestOutcomeSuccess},
		{RelayLogID: 9064, Time: now - 60, APIKeyID: 4, APIKeyName: "=SUM(1,1)", Outcome: model.RequestOutcomeSuccess},
		{RelayLogID: 9065, Time: now - 60, APIKeyID: 5, APIKeyName: "Zulu", Outcome: model.RequestOutcomeSuccess},
	}
	if err := dbpkg.GetDB().WithContext(ctx).Create(&facts).Error; err != nil {
		t.Fatalf("create tied usage facts: %v", err)
	}
	filter := UsageAnalyticsFilter{
		StartTime: now - 300,
		EndTime:   now + 1,
		Timezone:  "UTC",
		Scope:     UsageMetricScopeRequest,
	}

	var pagedIDs []int
	seen := make(map[int]struct{})
	for page := 1; page <= 3; page++ {
		result, err := UsageAnalyticsBreakdownSearchGet(
			ctx,
			filter,
			"api_key",
			"",
			page,
			2,
			"request_count",
			true,
		)
		if err != nil {
			t.Fatalf("breakdown page %d: %v", page, err)
		}
		if result.Total != int64(len(facts)) {
			t.Fatalf("page %d total = %d, want %d", page, result.Total, len(facts))
		}
		for _, item := range result.Items {
			if _, duplicate := seen[item.ID]; duplicate {
				t.Fatalf("duplicate item across pages: %+v", item)
			}
			seen[item.ID] = struct{}{}
			pagedIDs = append(pagedIDs, item.ID)
		}
	}
	wantIDs := []int{4, 1, 2, 3, 5}
	if !slices.Equal(pagedIDs, wantIDs) {
		t.Fatalf("unstable tied pagination: got %v want %v", pagedIDs, wantIDs)
	}

	exported, err := UsageAnalyticsBreakdownExportGet(
		ctx,
		filter,
		"api_key",
		"",
		"request_count",
		true,
	)
	if err != nil {
		t.Fatalf("export breakdown: %v", err)
	}
	exportedIDs := make([]int, 0, len(exported.Items))
	for _, item := range exported.Items {
		exportedIDs = append(exportedIDs, item.ID)
	}
	if exported.Total != int64(len(facts)) || !slices.Equal(exportedIDs, pagedIDs) {
		t.Fatalf("export order differs from complete pagination: paged=%v export=%+v", pagedIDs, exported)
	}

	filtered, err := UsageAnalyticsBreakdownExportGet(
		ctx,
		filter,
		"api_key",
		"alpha",
		"request_count",
		true,
	)
	if err != nil {
		t.Fatalf("filtered export breakdown: %v", err)
	}
	if filtered.Total != 2 || len(filtered.Items) != 2 ||
		filtered.Items[0].ID != 1 || filtered.Items[1].ID != 2 {
		t.Fatalf("filtered export lost search or stable order: %+v", filtered)
	}
}

func TestUsageBreakdownExportPagesBoundHighCardinality(t *testing.T) {
	ctx := setupSiteOpTestDB(t)
	now := time.Now().Unix()
	const factCount = usageBreakdownExportPageSize + 10
	facts := make([]model.UsageRequestFact, 0, factCount)
	for index := 1; index <= factCount; index++ {
		facts = append(facts, model.UsageRequestFact{
			RelayLogID:  940_000 + int64(index),
			Time:        now - 60,
			APIKeyID:    index,
			APIKeyName:  fmt.Sprintf("Key-%03d", index),
			Outcome:     model.RequestOutcomeSuccess,
			DurationMS:  int64(index),
			TokenSource: model.UsageValueSourceUnknown,
		})
	}
	if err := dbpkg.GetDB().WithContext(ctx).CreateInBatches(&facts, 100).Error; err != nil {
		t.Fatalf("create high-cardinality usage facts: %v", err)
	}
	filter := UsageAnalyticsFilter{
		StartTime: now - 300,
		EndTime:   now + 1,
		Timezone:  "UTC",
		Scope:     UsageMetricScopeRequest,
	}
	first, err := UsageAnalyticsBreakdownExportPageGet(
		ctx,
		filter,
		"api_key",
		"",
		1,
		"request_count",
		true,
	)
	if err != nil {
		t.Fatalf("first high-cardinality export page: %v", err)
	}
	second, err := UsageAnalyticsBreakdownExportPageGet(
		ctx,
		filter,
		"api_key",
		"",
		2,
		"request_count",
		true,
	)
	if err != nil {
		t.Fatalf("second high-cardinality export page: %v", err)
	}
	if first.Total != factCount ||
		first.PageSize != usageBreakdownExportPageSize ||
		len(first.Items) != usageBreakdownExportPageSize ||
		first.Items[0].ID != 1 ||
		first.Items[len(first.Items)-1].ID != usageBreakdownExportPageSize {
		t.Fatalf("unexpected first export page: %+v", first)
	}
	if second.Total != factCount ||
		second.PageSize != usageBreakdownExportPageSize ||
		len(second.Items) != 10 ||
		second.Items[0].ID != usageBreakdownExportPageSize+1 ||
		second.Items[len(second.Items)-1].ID != factCount {
		t.Fatalf("unexpected second export page: %+v", second)
	}
}

func TestUsageFactsPersistWhenRelayLogRetentionIsDisabled(t *testing.T) {
	ctx := setupSiteOpTestDB(t)
	resetRelayLogStateForTest()
	if err := settingRefreshCache(ctx); err != nil {
		t.Fatalf("settingRefreshCache failed: %v", err)
	}
	if err := SettingSetString(model.SettingKeyRelayLogKeepEnabled, "false"); err != nil {
		t.Fatalf("disable relay logs: %v", err)
	}

	if err := RelayLogAdd(ctx, model.RelayLog{
		ID:               9100,
		Time:             1_720_000_000,
		RequestModelName: "gpt",
		Outcome:          model.RequestOutcomeSuccess,
		Success:          true,
	}); err != nil {
		t.Fatalf("RelayLogAdd failed: %v", err)
	}
	if err := RelayLogFlushPending(ctx); err != nil {
		t.Fatalf("RelayLogFlushPending failed: %v", err)
	}

	var logCount int64
	if err := dbpkg.GetDB().WithContext(ctx).Model(&model.RelayLog{}).Count(&logCount).Error; err != nil {
		t.Fatalf("count relay logs: %v", err)
	}
	var factCount int64
	if err := dbpkg.GetDB().WithContext(ctx).Model(&model.UsageRequestFact{}).Count(&factCount).Error; err != nil {
		t.Fatalf("count usage facts: %v", err)
	}
	if logCount != 0 || factCount != 1 {
		t.Fatalf("unexpected retention behavior: logs=%d facts=%d", logCount, factCount)
	}
}

func TestUsageFactsKeepEnqueueTimeSiteDimensions(t *testing.T) {
	ctx := setupSiteOpTestDB(t)
	resetRelayLogStateForTest()
	fixture := createUsageAnalyticsFixture(t, ctx)

	if err := RelayLogAdd(ctx, model.RelayLog{
		ID:               9150,
		Time:             1_720_000_000,
		RequestModelName: "gpt",
		ChannelId:        fixture.channelA.ID,
		ChannelName:      fixture.channelA.Name,
		Outcome:          model.RequestOutcomeSuccess,
		Success:          true,
	}); err != nil {
		t.Fatalf("RelayLogAdd failed: %v", err)
	}

	site := model.Site{
		Name: "Replacement Site", Platform: model.SitePlatformNewAPI,
		BaseURL: "https://replacement.example.com", Enabled: true,
	}
	if err := dbpkg.GetDB().WithContext(ctx).Create(&site).Error; err != nil {
		t.Fatalf("create replacement site: %v", err)
	}
	account := model.SiteAccount{
		SiteID: site.ID, Name: "Replacement Account",
		CredentialType: model.SiteCredentialTypeAccessToken,
		AccessToken:    "token", Enabled: true,
	}
	if err := dbpkg.GetDB().WithContext(ctx).Create(&account).Error; err != nil {
		t.Fatalf("create replacement account: %v", err)
	}
	if err := dbpkg.GetDB().WithContext(ctx).
		Model(&model.SiteChannelBinding{}).
		Where("channel_id = ?", fixture.channelA.ID).
		Updates(map[string]any{
			"site_id":         site.ID,
			"site_account_id": account.ID,
		}).Error; err != nil {
		t.Fatalf("rebind channel: %v", err)
	}
	if err := RelayLogFlushPending(ctx); err != nil {
		t.Fatalf("RelayLogFlushPending failed: %v", err)
	}

	var fact model.UsageRequestFact
	if err := dbpkg.GetDB().WithContext(ctx).First(&fact, "relay_log_id = ?", 9150).Error; err != nil {
		t.Fatalf("load usage fact: %v", err)
	}
	if fact.SiteName != "Site A" || fact.SiteAccountName != "Account A" {
		t.Fatalf("usage dimensions drifted after rebind: %+v", fact)
	}
}

func TestUsageFactsNormalizeProviderCacheSemantics(t *testing.T) {
	openAI := usageFactsFromRelayLog(model.RelayLog{
		ID:               9160,
		InputTokens:      100,
		BillInputTokens:  intPtr(80),
		CacheReadTokens:  intPtr(20),
		CacheWriteTokens: intPtr(0),
		OutputTokens:     10,
	})
	if openAI.request.InputTokens != 100 {
		t.Fatalf("OpenAI cached input was double counted: %+v", openAI.request)
	}

	anthropic := usageFactsFromRelayLog(model.RelayLog{
		ID:               9161,
		InputTokens:      40,
		BillInputTokens:  intPtr(40),
		CacheReadTokens:  intPtr(100),
		CacheWriteTokens: intPtr(10),
		OutputTokens:     5,
	})
	if anthropic.request.InputTokens != 150 {
		t.Fatalf("Anthropic cached input was omitted: %+v", anthropic.request)
	}
	metrics := usageMetricsFromAggregate(usageAggregateRow{
		MetricCount:  2,
		InputTokens:  openAI.request.InputTokens + anthropic.request.InputTokens,
		OutputTokens: openAI.request.OutputTokens + anthropic.request.OutputTokens,
	}, UsageMetricScopeRequest)
	if metrics.TotalTokens != 265 {
		t.Fatalf("unexpected normalized total tokens: %+v", metrics)
	}
}

func TestUsageFactsAttributeEachAttemptSnapshot(t *testing.T) {
	record := usageFactsFromRelayLog(model.RelayLog{
		ID:               9170,
		RequestModelName: "gpt",
		ChannelId:        2,
		Outcome:          model.RequestOutcomeSuccess,
		Attempts: []model.ChannelAttempt{
			{
				ChannelID: 1, ChannelName: "first", ModelName: "upstream-a",
				AttemptNum: 1, Status: model.AttemptFailed,
				Usage: &model.AttemptUsageSnapshot{
					InputTokens: 50, OutputTokens: 1, CostUSD: 0.1,
					TokenSource: model.UsageValueSourceReported,
				},
			},
			{
				ChannelID: 2, ChannelName: "second", ModelName: "upstream-b",
				AttemptNum: 2, Status: model.AttemptSuccess,
				Usage: &model.AttemptUsageSnapshot{
					InputTokens: 100, OutputTokens: 20, CostUSD: 0.2,
					TokenSource: model.UsageValueSourceReported,
				},
			},
		},
	})
	if len(record.attempts) != 2 ||
		record.attempts[0].InputTokens != 50 ||
		record.attempts[1].InputTokens != 100 {
		t.Fatalf("attempt usage snapshots were not preserved: %+v", record.attempts)
	}
	if record.request.InputTokens != 150 ||
		record.request.OutputTokens != 21 ||
		math.Abs(record.request.CostUSD-0.3) > 1e-9 {
		t.Fatalf("request usage did not include all measured attempts: %+v", record.request)
	}
}

func TestUsageFactQueueAppliesBackpressureInsteadOfDropping(t *testing.T) {
	ctx := setupSiteOpTestDB(t)
	usageFactsResetForTest()
	defer usageFactsResetForTest()
	usagePendingLock.Lock()
	for i := 0; i < usageFactQueueSize; i++ {
		usagePending = append(usagePending, usagePendingRecord{
			request: model.UsageRequestFact{
				RelayLogID: int64(20_000 + i),
				Time:       1_720_000_000,
				Outcome:    model.RequestOutcomeSuccess,
			},
		})
	}
	usagePendingLock.Unlock()

	if err := enqueueUsageFacts(ctx, usagePendingRecord{
		request: model.UsageRequestFact{
			RelayLogID: 30_000,
			Time:       1_720_000_001,
			Outcome:    model.RequestOutcomeSuccess,
		},
	}); err != nil {
		t.Fatalf("enqueueUsageFacts failed under pressure: %v", err)
	}
	var persisted int64
	if err := dbpkg.GetDB().WithContext(ctx).Model(&model.UsageRequestFact{}).Count(&persisted).Error; err != nil {
		t.Fatalf("count persisted facts: %v", err)
	}
	if persisted != usageFactBatchSize {
		t.Fatalf("backpressure did not flush one batch: got %d want %d", persisted, usageFactBatchSize)
	}
	if UsageFactsPendingLen() != usageFactQueueSize-usageFactBatchSize+1 {
		t.Fatalf("unexpected pending fact count: %d", UsageFactsPendingLen())
	}
}

func TestUsageDailyAggregateSurvivesHourlyRetentionAndServesAnalytics(t *testing.T) {
	ctx := setupSiteOpTestDB(t)
	if err := settingRefreshCache(ctx); err != nil {
		t.Fatalf("settingRefreshCache failed: %v", err)
	}
	oldDay := time.Now().UTC().AddDate(0, 0, -120)
	oldDay = time.Date(oldDay.Year(), oldDay.Month(), oldDay.Day(), 12, 0, 0, 0, time.UTC)
	fact := model.UsageRequestFact{
		RelayLogID:     9200,
		Time:           oldDay.Unix(),
		RequestModel:   "legacy-model",
		ActualModel:    "legacy-upstream",
		CanonicalModel: "legacy-model",
		Outcome:        model.RequestOutcomeSuccess,
		InputTokens:    100,
		OutputTokens:   25,
		CostUSD:        0.5,
		DurationMS:     750,
		FTUTMS:         120,
		FTUTKnown:      true,
		TokenSource:    model.UsageValueSourceReported,
		PriceSource:    model.PriceQuoteSourceGlobal,
	}
	if err := dbpkg.GetDB().WithContext(ctx).Create(&fact).Error; err != nil {
		t.Fatalf("create usage fact: %v", err)
	}
	processed, err := UsageAggregatePending(ctx, 100)
	if err != nil || processed != 1 {
		t.Fatalf("aggregate pending: processed=%d err=%v", processed, err)
	}
	processed, err = UsageAggregatePending(ctx, 100)
	if err != nil || processed != 0 {
		t.Fatalf("aggregate was not idempotent: processed=%d err=%v", processed, err)
	}

	start := time.Date(oldDay.Year(), oldDay.Month(), oldDay.Day(), 0, 0, 0, 0, time.UTC)
	filter := UsageAnalyticsFilter{
		StartTime: start.Unix(),
		EndTime:   start.AddDate(0, 0, 1).Unix(),
		Timezone:  "UTC",
		Scope:     UsageMetricScopeRequest,
	}
	summary, err := UsageAnalyticsSummaryGet(ctx, filter)
	if err != nil {
		t.Fatalf("summary with source fact and aggregate: %v", err)
	}
	if summary.RequestCount != 1 {
		t.Fatalf("fact and aggregate were double counted: %+v", summary)
	}

	if err := dbpkg.GetDB().WithContext(ctx).
		Delete(&model.UsageRequestFact{}, "relay_log_id = ?", fact.RelayLogID).Error; err != nil {
		t.Fatalf("delete source fact: %v", err)
	}
	if _, err := UsageAggregateRetention(ctx, time.Now(), 90); err != nil {
		t.Fatalf("aggregate retention: %v", err)
	}
	var hourlyCount, dailyCount int64
	if err := dbpkg.GetDB().WithContext(ctx).Model(&model.UsageAggregate{}).
		Where("granularity = ?", model.UsageAggregateHourly).
		Count(&hourlyCount).Error; err != nil {
		t.Fatalf("count hourly aggregates: %v", err)
	}
	if err := dbpkg.GetDB().WithContext(ctx).Model(&model.UsageAggregate{}).
		Where("granularity = ?", model.UsageAggregateDaily).
		Count(&dailyCount).Error; err != nil {
		t.Fatalf("count daily aggregates: %v", err)
	}
	if hourlyCount != 0 || dailyCount != 1 {
		t.Fatalf("unexpected retained aggregates: hourly=%d daily=%d", hourlyCount, dailyCount)
	}

	summary, err = UsageAnalyticsSummaryGet(ctx, filter)
	if err != nil {
		t.Fatalf("summary from daily aggregate: %v", err)
	}
	if summary.RequestCount != 1 || summary.TotalTokens != 125 || math.Abs(summary.CostUSD-0.5) > 1e-9 {
		t.Fatalf("unexpected aggregate summary: %+v", summary)
	}
	series, err := UsageAnalyticsTimeseriesGet(ctx, filter)
	if err != nil {
		t.Fatalf("timeseries from daily aggregate: %v", err)
	}
	if len(series.Points) != 1 || series.Points[0].RequestCount != 1 {
		t.Fatalf("unexpected aggregate series: %+v", series)
	}
	breakdown, err := UsageAnalyticsBreakdownGet(
		ctx,
		filter,
		"canonical_model",
		1,
		20,
		"request_count",
		true,
	)
	if err != nil {
		t.Fatalf("breakdown from daily aggregate: %v", err)
	}
	if breakdown.Total != 1 || len(breakdown.Items) != 1 || breakdown.Items[0].Name != "legacy-model" {
		t.Fatalf("unexpected aggregate breakdown: %+v", breakdown)
	}
	item := breakdown.Items[0]
	if item.MetricCount != 1 ||
		item.RequestCount != 1 ||
		item.SuccessCount != 1 ||
		item.InputTokens != 100 ||
		item.OutputTokens != 25 ||
		item.TotalTokens != 125 ||
		math.Abs(item.CostUSD-0.5) > 1e-9 ||
		math.Abs(item.AverageDurationMS-750) > 1e-9 ||
		item.P95DurationMS != 1000 ||
		math.Abs(item.AverageFTUTMS-120) > 1e-9 ||
		item.P95FTUTMS != 250 {
		t.Fatalf("aggregate breakdown lost metrics: %+v", item)
	}
	breakdown, err = UsageAnalyticsBreakdownSearchGet(
		ctx,
		filter,
		"canonical_model",
		"no-match",
		1,
		20,
		"request_count",
		true,
	)
	if err != nil {
		t.Fatalf("search aggregate breakdown: %v", err)
	}
	if breakdown.Total != 0 || len(breakdown.Items) != 0 {
		t.Fatalf("aggregate search did not filter rows: %+v", breakdown)
	}
}

func TestUsageBreakdownMergesFactAndDailyAggregateP95(t *testing.T) {
	ctx := setupSiteOpTestDB(t)
	now := time.Now().UTC()
	oldDay := now.AddDate(0, 0, -120)
	oldBucket := time.Date(oldDay.Year(), oldDay.Month(), oldDay.Day(), 0, 0, 0, 0, time.UTC)

	durationHistogram := make([]uint64, len(usageLatencyBucketUpperBounds)+1)
	durationHistogram[0] = 19
	ftutHistogram := make([]uint64, len(usageLatencyBucketUpperBounds)+1)
	ftutHistogram[0] = 18
	aggregate := model.UsageAggregate{
		AggregateKey:      "breakdown-mixed-p95",
		Granularity:       model.UsageAggregateDaily,
		MetricScope:       string(UsageMetricScopeRequest),
		BucketStart:       oldBucket.Unix(),
		APIKeyID:          71,
		APIKeyName:        "Mixed P95",
		MetricCount:       19,
		SuccessCount:      19,
		DurationSumMS:     1900,
		DurationMaxMS:     100,
		DurationHistogram: durationHistogram,
		FTUTSumMS:         1800,
		FTUTMaxMS:         100,
		FTUTSamples:       18,
		FTUTHistogram:     ftutHistogram,
		UnknownTokenCount: 19,
		UnknownPriceCount: 19,
	}
	if err := dbpkg.GetDB().WithContext(ctx).Create(&aggregate).Error; err != nil {
		t.Fatalf("create daily usage aggregate: %v", err)
	}
	fact := model.UsageRequestFact{
		RelayLogID:  9250,
		Time:        now.Add(-time.Minute).Unix(),
		APIKeyID:    aggregate.APIKeyID,
		APIKeyName:  aggregate.APIKeyName,
		Outcome:     model.RequestOutcomeSuccess,
		DurationMS:  5000,
		FTUTMS:      5000,
		FTUTKnown:   true,
		TokenSource: model.UsageValueSourceUnknown,
	}
	if err := dbpkg.GetDB().WithContext(ctx).Create(&fact).Error; err != nil {
		t.Fatalf("create current usage fact: %v", err)
	}

	breakdown, err := UsageAnalyticsBreakdownGet(
		ctx,
		UsageAnalyticsFilter{
			StartTime: oldBucket.Unix(),
			EndTime:   now.Add(time.Second).Unix(),
			Timezone:  "UTC",
			Scope:     UsageMetricScopeRequest,
		},
		"api_key",
		1,
		20,
		"request_count",
		true,
	)
	if err != nil {
		t.Fatalf("mixed usage breakdown: %v", err)
	}
	if breakdown.Total != 1 || len(breakdown.Items) != 1 {
		t.Fatalf("unexpected mixed usage breakdown: %+v", breakdown)
	}
	item := breakdown.Items[0]
	if item.RequestCount != 20 ||
		item.P95DurationMS != 100 ||
		item.FTUTSamples != 19 ||
		item.P95FTUTMS != 5000 {
		t.Fatalf("mixed fact/aggregate P95 was not merged: %+v", item)
	}
}

func TestUsageCombinedBreakdownPaginatesAndSortsInDatabase(t *testing.T) {
	ctx := setupSiteOpTestDB(t)
	now := time.Now().UTC()
	oldDay := now.AddDate(0, 0, -120)
	oldBucket := time.Date(oldDay.Year(), oldDay.Month(), oldDay.Day(), 0, 0, 0, 0, time.UTC)
	aggregates := []model.UsageAggregate{
		{
			AggregateKey: "combined-page-1", Granularity: model.UsageAggregateDaily,
			MetricScope: string(UsageMetricScopeRequest), BucketStart: oldBucket.Unix(),
			APIKeyID: 1, APIKeyName: "Alpha", MetricCount: 2, SuccessCount: 2,
		},
		{
			AggregateKey: "combined-page-2", Granularity: model.UsageAggregateDaily,
			MetricScope: string(UsageMetricScopeRequest), BucketStart: oldBucket.Unix(),
			APIKeyID: 2, APIKeyName: "Beta", MetricCount: 2, SuccessCount: 2,
		},
		{
			AggregateKey: "combined-page-3", Granularity: model.UsageAggregateDaily,
			MetricScope: string(UsageMetricScopeRequest), BucketStart: oldBucket.Unix(),
			APIKeyID: 3, APIKeyName: "Alpha", MetricCount: 1, SuccessCount: 1,
		},
		{
			AggregateKey: "combined-page-5", Granularity: model.UsageAggregateDaily,
			MetricScope: string(UsageMetricScopeRequest), BucketStart: oldBucket.Unix(),
			APIKeyID: 5, APIKeyName: "=Formula", MetricCount: 1, SuccessCount: 1,
		},
	}
	if err := dbpkg.GetDB().WithContext(ctx).Create(&aggregates).Error; err != nil {
		t.Fatalf("create combined pagination aggregates: %v", err)
	}
	facts := []model.UsageRequestFact{
		{
			RelayLogID: 9261, Time: now.Add(-time.Minute).Unix(),
			APIKeyID: 1, APIKeyName: "Alpha", Outcome: model.RequestOutcomeSuccess,
		},
		{
			RelayLogID: 9264, Time: now.Add(-time.Minute).Unix(),
			APIKeyID: 4, APIKeyName: "Zulu", Outcome: model.RequestOutcomeSuccess,
		},
	}
	if err := dbpkg.GetDB().WithContext(ctx).Create(&facts).Error; err != nil {
		t.Fatalf("create combined pagination facts: %v", err)
	}
	filter := UsageAnalyticsFilter{
		StartTime: oldBucket.Unix(),
		EndTime:   now.Add(time.Second).Unix(),
		Timezone:  "UTC",
		Scope:     UsageMetricScopeRequest,
	}

	var pagedIDs []int
	for page := 1; page <= 3; page++ {
		result, err := UsageAnalyticsBreakdownSearchGet(
			ctx,
			filter,
			"api_key",
			"",
			page,
			2,
			"request_count",
			true,
		)
		if err != nil {
			t.Fatalf("combined breakdown page %d: %v", page, err)
		}
		if result.Total != 5 {
			t.Fatalf("combined page %d total = %d, want 5", page, result.Total)
		}
		for _, item := range result.Items {
			pagedIDs = append(pagedIDs, item.ID)
		}
	}
	wantIDs := []int{1, 2, 5, 3, 4}
	if !slices.Equal(pagedIDs, wantIDs) {
		t.Fatalf("combined pagination order = %v, want %v", pagedIDs, wantIDs)
	}

	exported, err := UsageAnalyticsBreakdownExportPageGet(
		ctx,
		filter,
		"api_key",
		"",
		1,
		"request_count",
		true,
	)
	if err != nil {
		t.Fatalf("combined export page: %v", err)
	}
	exportedIDs := make([]int, 0, len(exported.Items))
	for _, item := range exported.Items {
		exportedIDs = append(exportedIDs, item.ID)
	}
	if !slices.Equal(exportedIDs, wantIDs) {
		t.Fatalf("combined export order = %v, want %v", exportedIDs, wantIDs)
	}

	filtered, err := UsageAnalyticsBreakdownSearchGet(
		ctx,
		filter,
		"api_key",
		"alpha",
		1,
		20,
		"request_count",
		true,
	)
	if err != nil {
		t.Fatalf("search combined breakdown: %v", err)
	}
	if filtered.Total != 2 ||
		len(filtered.Items) != 2 ||
		filtered.Items[0].ID != 1 ||
		filtered.Items[1].ID != 3 {
		t.Fatalf("combined search lost server filtering: %+v", filtered)
	}
}

func TestUsageHourlyBucketRangeHandlesDSTTransitions(t *testing.T) {
	location, err := time.LoadLocation("America/New_York")
	if err != nil {
		t.Fatalf("load timezone: %v", err)
	}
	for _, test := range []struct {
		name       string
		start      time.Time
		wantPoints int
	}{
		{
			name:       "spring forward",
			start:      time.Date(2026, 3, 8, 0, 0, 0, 0, location),
			wantPoints: 23,
		},
		{
			name:       "fall back",
			start:      time.Date(2026, 11, 1, 0, 0, 0, 0, location),
			wantPoints: 25,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			filter := UsageAnalyticsFilter{
				StartTime: test.start.Unix(),
				EndTime:   test.start.AddDate(0, 0, 1).Unix(),
			}
			buckets := usageBucketRange(filter, location, "hour")
			if len(buckets) != test.wantPoints {
				t.Fatalf("hour bucket count = %d, want %d", len(buckets), test.wantPoints)
			}
			seen := make(map[int64]struct{}, len(buckets))
			for _, bucket := range buckets {
				if _, ok := seen[bucket]; ok {
					t.Fatalf("duplicate absolute bucket %d", bucket)
				}
				seen[bucket] = struct{}{}
			}
		})
	}
}

func TestUsageAggregatePendingIsConcurrentSafe(t *testing.T) {
	ctx := setupSiteOpTestDB(t)
	if err := dbpkg.GetDB().WithContext(ctx).Create(&model.UsageRequestFact{
		RelayLogID: 9300,
		Time:       time.Now().Unix(),
		Outcome:    model.RequestOutcomeSuccess,
	}).Error; err != nil {
		t.Fatalf("create usage fact: %v", err)
	}

	const workers = 5
	var wg sync.WaitGroup
	results := make(chan int, workers)
	errs := make(chan error, workers)
	for range workers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			count, err := UsageAggregatePending(ctx, 100)
			results <- count
			errs <- err
		}()
	}
	wg.Wait()
	close(results)
	close(errs)

	totalProcessed := 0
	for count := range results {
		totalProcessed += count
	}
	for err := range errs {
		if err != nil {
			t.Fatalf("concurrent aggregate failed: %v", err)
		}
	}
	if totalProcessed != 1 {
		t.Fatalf("fact was aggregated %d times", totalProcessed)
	}
	var aggregates []model.UsageAggregate
	if err := dbpkg.GetDB().WithContext(ctx).
		Where("metric_scope = ?", UsageMetricScopeRequest).
		Find(&aggregates).Error; err != nil {
		t.Fatalf("load aggregates: %v", err)
	}
	for _, aggregate := range aggregates {
		if aggregate.MetricCount != 1 {
			t.Fatalf("aggregate double counted fact: %+v", aggregate)
		}
	}
}

type usageAnalyticsFixture struct {
	channelA model.Channel
	channelB model.Channel
}

func createUsageAnalyticsFixture(t *testing.T, ctx context.Context) usageAnalyticsFixture {
	t.Helper()
	createManagedChannel := func(siteName, accountName, channelName string) model.Channel {
		site := model.Site{
			Name:     siteName,
			Platform: model.SitePlatformNewAPI,
			BaseURL:  "https://" + strings.ToLower(siteName) + ".example.com",
			Enabled:  true,
		}
		if err := dbpkg.GetDB().WithContext(ctx).Create(&site).Error; err != nil {
			t.Fatalf("create site: %v", err)
		}
		account := model.SiteAccount{
			SiteID:         site.ID,
			Name:           accountName,
			CredentialType: model.SiteCredentialTypeAccessToken,
			AccessToken:    "token",
			Enabled:        true,
		}
		if err := dbpkg.GetDB().WithContext(ctx).Create(&account).Error; err != nil {
			t.Fatalf("create account: %v", err)
		}
		channel := model.Channel{
			Name:    channelName,
			Type:    outbound.OutboundTypeOpenAIChat,
			Enabled: true,
		}
		if err := dbpkg.GetDB().WithContext(ctx).Create(&channel).Error; err != nil {
			t.Fatalf("create channel: %v", err)
		}
		binding := model.SiteChannelBinding{
			SiteID:        site.ID,
			SiteAccountID: account.ID,
			GroupKey:      model.SiteDefaultGroupKey,
			ChannelID:     channel.ID,
		}
		if err := dbpkg.GetDB().WithContext(ctx).Create(&binding).Error; err != nil {
			t.Fatalf("create binding: %v", err)
		}
		return channel
	}
	return usageAnalyticsFixture{
		channelA: createManagedChannel("Site A", "Account A", "Channel A"),
		channelB: createManagedChannel("Site B", "Account B", "Channel B"),
	}
}
