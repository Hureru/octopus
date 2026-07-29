package op

import (
	"context"
	"testing"

	dbpkg "github.com/bestruirui/octopus/internal/db"
	"github.com/bestruirui/octopus/internal/model"
)

func TestRelayLogRepairPreviewAuditsWithoutChangingRows(t *testing.T) {
	ctx := setupSiteOpTestDB(t)
	resetRelayLogStateForTest()
	seedRelayLogRepairRows(t, ctx)

	preview, err := RelayLogRepairPreviewRun(ctx, RelayLogRepairFilter{})
	if err != nil {
		t.Fatalf("RelayLogRepairPreviewRun failed: %v", err)
	}
	if preview.AuditID == "" || preview.Matched != 1 || preview.Excluded != 2 {
		t.Fatalf("unexpected preview: %+v", preview)
	}
	if preview.Reasons["missing_finish_reason"] != 1 || preview.Reasons["not_responses_request"] != 1 {
		t.Fatalf("unexpected exclusion reasons: %+v", preview.Reasons)
	}

	var audit model.RelayLogRepairAudit
	if err := dbpkg.GetDB().WithContext(ctx).Where("batch_id = ?", preview.AuditID).First(&audit).Error; err != nil {
		t.Fatalf("query preview audit: %v", err)
	}
	if !audit.DryRun || audit.Matched != 1 || audit.Updated != 0 || audit.Excluded != 2 {
		t.Fatalf("unexpected preview audit: %+v", audit)
	}

	var row model.RelayLog
	if err := dbpkg.GetDB().WithContext(ctx).First(&row, 1001).Error; err != nil {
		t.Fatalf("query candidate: %v", err)
	}
	if row.Success || row.Outcome != model.RequestOutcomeFailed || row.Error != "context canceled" {
		t.Fatalf("preview mutated candidate: %+v", row)
	}
}

func TestRelayLogRepairExecuteIsAuditedAndIdempotent(t *testing.T) {
	ctx := setupSiteOpTestDB(t)
	resetRelayLogStateForTest()
	seedRelayLogRepairRows(t, ctx)
	seedRelayLogRepairUsage(t, ctx)

	result, err := RelayLogRepairExecute(ctx, RelayLogRepairFilter{})
	if err != nil {
		t.Fatalf("RelayLogRepairExecute failed: %v", err)
	}
	if result.BatchID == "" || result.Matched != 1 || result.Updated != 1 {
		t.Fatalf("unexpected repair result: %+v", result)
	}

	var row model.RelayLog
	if err := dbpkg.GetDB().WithContext(ctx).First(&row, 1001).Error; err != nil {
		t.Fatalf("query repaired row: %v", err)
	}
	if !row.Success ||
		row.Outcome != model.RequestOutcomeSuccess ||
		row.OriginalOutcome != model.RequestOutcomeFailed ||
		row.OriginalError != "context canceled" ||
		row.Error != "" ||
		row.RepairBatchID != result.BatchID ||
		row.CompletionEvidence != model.CompletionEvidenceHistoricalRule {
		t.Fatalf("unexpected repaired row: %+v", row)
	}

	var audit model.RelayLogRepairAudit
	if err := dbpkg.GetDB().WithContext(ctx).Where("batch_id = ?", result.BatchID).First(&audit).Error; err != nil {
		t.Fatalf("query execute audit: %v", err)
	}
	if audit.DryRun || audit.Matched != 1 || audit.Updated != 1 {
		t.Fatalf("unexpected execute audit: %+v", audit)
	}
	assertRelayLogRepairUsage(t, ctx)

	second, err := RelayLogRepairExecute(ctx, RelayLogRepairFilter{})
	if err != nil {
		t.Fatalf("second RelayLogRepairExecute failed: %v", err)
	}
	if second.Matched != 0 || second.Updated != 0 {
		t.Fatalf("repair was not idempotent: %+v", second)
	}
	assertRelayLogRepairUsage(t, ctx)
}

func TestRelayLogRepairRollsBackWhenDailyAggregateIsMissing(t *testing.T) {
	ctx := setupSiteOpTestDB(t)
	resetRelayLogStateForTest()
	seedRelayLogRepairRows(t, ctx)
	seedRelayLogRepairUsage(t, ctx)
	if err := dbpkg.GetDB().WithContext(ctx).
		Where("granularity = ?", model.UsageAggregateDaily).
		Delete(&model.UsageAggregate{}).Error; err != nil {
		t.Fatalf("delete daily aggregate: %v", err)
	}

	if _, err := RelayLogRepairExecute(ctx, RelayLogRepairFilter{}); err == nil {
		t.Fatal("repair succeeded without the required daily aggregate")
	}
	var row model.RelayLog
	if err := dbpkg.GetDB().WithContext(ctx).First(&row, 1001).Error; err != nil {
		t.Fatalf("load relay log: %v", err)
	}
	if row.Success || row.Outcome != model.RequestOutcomeFailed || row.RepairBatchID != "" {
		t.Fatalf("failed repair did not roll back relay log: %+v", row)
	}
	var request model.UsageRequestFact
	if err := dbpkg.GetDB().WithContext(ctx).First(&request, "relay_log_id = ?", 1001).Error; err != nil {
		t.Fatalf("load request fact: %v", err)
	}
	if request.Outcome != model.RequestOutcomeFailed {
		t.Fatalf("failed repair did not roll back request fact: %+v", request)
	}
}

func TestRelayLogRepairAllowsExpiredHourlyAggregateToBeMissing(t *testing.T) {
	ctx := setupSiteOpTestDB(t)
	resetRelayLogStateForTest()
	seedRelayLogRepairRows(t, ctx)
	seedRelayLogRepairUsage(t, ctx)
	if err := dbpkg.GetDB().WithContext(ctx).
		Where("granularity = ?", model.UsageAggregateHourly).
		Delete(&model.UsageAggregate{}).Error; err != nil {
		t.Fatalf("delete hourly aggregate: %v", err)
	}

	result, err := RelayLogRepairExecute(ctx, RelayLogRepairFilter{})
	if err != nil {
		t.Fatalf("repair with expired hourly aggregate: %v", err)
	}
	if result.Updated != 1 {
		t.Fatalf("unexpected repair result: %+v", result)
	}
	var request model.UsageRequestFact
	if err := dbpkg.GetDB().WithContext(ctx).First(&request, "relay_log_id = ?", 1001).Error; err != nil {
		t.Fatalf("load request fact: %v", err)
	}
	if request.Outcome != model.RequestOutcomeSuccess {
		t.Fatalf("request fact was not repaired: %+v", request)
	}
}

func seedRelayLogRepairRows(t *testing.T, ctx context.Context) {
	t.Helper()
	rows := []model.RelayLog{
		{
			ID:               1001,
			Time:             1001,
			RequestModelName: "grok",
			ChannelName:      "primary",
			RequestContent:   `{"stream":true,"input":"hello"}`,
			ResponseContent:  `{"choices":[{"finish_reason":"stop"}]}`,
			OutputTokens:     5,
			Error:            "context canceled",
			Outcome:          model.RequestOutcomeFailed,
		},
		{
			ID:               1002,
			Time:             1002,
			RequestModelName: "partial",
			RequestContent:   `{"stream":true,"input":"hello"}`,
			ResponseContent:  `{"choices":[{}]}`,
			OutputTokens:     2,
			Error:            "context canceled",
			Outcome:          model.RequestOutcomeFailed,
		},
		{
			ID:               1003,
			Time:             1003,
			RequestModelName: "chat",
			RequestContent:   `{"stream":true,"messages":[{"role":"user","content":"hello"}]}`,
			ResponseContent:  `{"choices":[{"finish_reason":"stop"}]}`,
			OutputTokens:     3,
			Error:            "context canceled",
			Outcome:          model.RequestOutcomeFailed,
		},
	}
	if err := dbpkg.GetDB().WithContext(ctx).Create(&rows).Error; err != nil {
		t.Fatalf("seed relay logs: %v", err)
	}
}

func seedRelayLogRepairUsage(t *testing.T, ctx context.Context) {
	t.Helper()
	request := model.UsageRequestFact{
		RelayLogID:   1001,
		Time:         1001,
		RequestModel: "grok",
		Outcome:      model.RequestOutcomeFailed,
		TokenSource:  model.UsageValueSourceReported,
	}
	attempt := model.UsageAttemptFact{
		RelayLogID:    1001,
		AttemptNumber: 1,
		Time:          1001,
		RequestModel:  "grok",
		Status:        model.AttemptFailed,
		Outcome:       model.RequestOutcomeFailed,
		Attribution:   model.AttemptAttributionUpstream,
		TokenSource:   model.UsageValueSourceReported,
	}
	if err := dbpkg.GetDB().WithContext(ctx).Create(&request).Error; err != nil {
		t.Fatalf("seed request fact: %v", err)
	}
	if err := dbpkg.GetDB().WithContext(ctx).Create(&attempt).Error; err != nil {
		t.Fatalf("seed attempt fact: %v", err)
	}
	if processed, err := UsageAggregatePending(ctx, 100); err != nil || processed != 2 {
		t.Fatalf("aggregate repair facts: processed=%d err=%v", processed, err)
	}
}

func assertRelayLogRepairUsage(t *testing.T, ctx context.Context) {
	t.Helper()
	var request model.UsageRequestFact
	if err := dbpkg.GetDB().WithContext(ctx).First(&request, "relay_log_id = ?", 1001).Error; err != nil {
		t.Fatalf("load repaired request fact: %v", err)
	}
	if request.Outcome != model.RequestOutcomeSuccess || request.AggregatedAt == nil {
		t.Fatalf("request fact was not repaired: %+v", request)
	}

	var attempt model.UsageAttemptFact
	if err := dbpkg.GetDB().WithContext(ctx).
		First(&attempt, "relay_log_id = ? AND attempt_number = ?", 1001, 1).Error; err != nil {
		t.Fatalf("load repaired attempt fact: %v", err)
	}
	if attempt.Status != model.AttemptSuccess ||
		attempt.Outcome != model.RequestOutcomeSuccess ||
		attempt.Attribution != model.AttemptAttributionNone ||
		attempt.AggregatedAt == nil {
		t.Fatalf("attempt fact was not repaired: %+v", attempt)
	}

	facts := []usageAggregateFact{
		usageAggregateFactFromRequest(request),
		usageAggregateFactFromAttempt(attempt),
	}
	for _, fact := range facts {
		for _, granularity := range []model.UsageAggregateGranularity{
			model.UsageAggregateHourly,
			model.UsageAggregateDaily,
		} {
			key := usageAggregateKey(fact, granularity, usageAggregateBucketStart(fact.time, granularity))
			var aggregate model.UsageAggregate
			if err := dbpkg.GetDB().WithContext(ctx).First(&aggregate, "aggregate_key = ?", key).Error; err != nil {
				t.Fatalf("load repaired %s aggregate %s: %v", fact.scope, granularity, err)
			}
			if aggregate.SuccessCount != 1 || aggregate.FailedCount != 0 || aggregate.MetricCount != 1 {
				t.Fatalf("aggregate outcome was not shifted exactly once: %+v", aggregate)
			}
		}
	}
}
