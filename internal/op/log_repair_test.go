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

	second, err := RelayLogRepairExecute(ctx, RelayLogRepairFilter{})
	if err != nil {
		t.Fatalf("second RelayLogRepairExecute failed: %v", err)
	}
	if second.Matched != 0 || second.Updated != 0 {
		t.Fatalf("repair was not idempotent: %+v", second)
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
