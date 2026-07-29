package handlers

import (
	"bytes"
	"encoding/csv"
	"testing"

	"github.com/bestruirui/octopus/internal/op"
)

func TestWriteUsageAnalyticsCSVIncludesAllMetricsAndSanitizesFormula(t *testing.T) {
	item := op.UsageBreakdownItem{
		ID:   42,
		Name: "=HYPERLINK(\"https://example.invalid\")",
		UsageAnalyticsMetrics: op.UsageAnalyticsMetrics{
			MetricCount:        11,
			RequestCount:       10,
			AttemptCount:       9,
			SuccessCount:       8,
			FailedCount:        7,
			CanceledCount:      6,
			IndeterminateCount: 5,
			SuccessRate:        0.75,
			InputTokens:        4,
			OutputTokens:       3,
			CacheReadTokens:    2,
			CacheWriteTokens:   1,
			TotalTokens:        7,
			CostUSD:            0.125,
			AverageDurationMS:  123.5,
			P95DurationMS:      456,
			AverageFTUTMS:      78.25,
			P95FTUTMS:          90,
			FTUTSamples:        4,
		},
	}

	var output bytes.Buffer
	if err := writeUsageAnalyticsCSV(
		&output,
		op.UsageMetricScopeRequest,
		"api_key",
		[]op.UsageBreakdownItem{item},
	); err != nil {
		t.Fatalf("write CSV: %v", err)
	}
	records, err := csv.NewReader(&output).ReadAll()
	if err != nil {
		t.Fatalf("parse CSV: %v", err)
	}
	if len(records) != 2 {
		t.Fatalf("CSV records = %d, want 2", len(records))
	}
	header := records[0]
	row := records[1]
	if len(header) != len(row) {
		t.Fatalf("header/row width mismatch: %d/%d", len(header), len(row))
	}
	values := make(map[string]string, len(header))
	for index, column := range header {
		values[column] = row[index]
	}
	if values["metric_scope"] != "request" ||
		values["dimension"] != "api_key" ||
		values["id"] != "42" ||
		values["name"] != "'"+item.Name {
		t.Fatalf("unexpected CSV identity columns: %#v", values)
	}
	for _, column := range usageAnalyticsMetricCSVColumns {
		if _, ok := values[column]; !ok {
			t.Fatalf("missing metrics column %q", column)
		}
	}
	if values["metric_count"] != "11" ||
		values["success_rate"] != "0.75" ||
		values["cost_usd"] != "0.125" ||
		values["average_ftut_ms"] != "78.25" {
		t.Fatalf("metrics were not encoded exactly: %#v", values)
	}
	if got := sanitizeUsageCSVCell(" \t+cmd"); got != "' \t+cmd" {
		t.Fatalf("leading whitespace bypassed formula protection: %q", got)
	}
}

func TestWriteUsageAnalyticsCSVPagesStreamsCompleteStableOrder(t *testing.T) {
	firstPage := op.UsageBreakdown{
		Scope:     op.UsageMetricScopeAttempt,
		Dimension: "channel",
		Page:      1,
		PageSize:  2,
		Total:     3,
		Items: []op.UsageBreakdownItem{
			{ID: 1, Name: "Alpha"},
			{ID: 2, Name: "Beta"},
		},
	}
	loadedPages := 0
	var output bytes.Buffer
	err := writeUsageAnalyticsCSVPages(
		&output,
		firstPage,
		func(page int) (op.UsageBreakdown, error) {
			loadedPages++
			if page != 2 {
				t.Fatalf("loaded page %d, want 2", page)
			}
			return op.UsageBreakdown{
				Scope:     firstPage.Scope,
				Dimension: firstPage.Dimension,
				Page:      page,
				PageSize:  2,
				Total:     firstPage.Total,
				Items:     []op.UsageBreakdownItem{{ID: 3, Name: "Gamma"}},
			}, nil
		},
	)
	if err != nil {
		t.Fatalf("write paged CSV: %v", err)
	}
	if loadedPages != 1 {
		t.Fatalf("loaded %d additional pages, want 1", loadedPages)
	}
	records, err := csv.NewReader(&output).ReadAll()
	if err != nil {
		t.Fatalf("parse paged CSV: %v", err)
	}
	if len(records) != 4 {
		t.Fatalf("CSV records = %d, want 4", len(records))
	}
	for index, wantID := range []string{"1", "2", "3"} {
		if records[index+1][2] != wantID {
			t.Fatalf("row %d id = %q, want %q", index, records[index+1][2], wantID)
		}
	}
}
