package op

import (
	"testing"

	"gorm.io/gorm/clause"

	dbpkg "github.com/bestruirui/octopus/internal/db"
	"github.com/bestruirui/octopus/internal/model"
)

// 回归 issue #114：同一小时桶第二次落库必须累加，而不是报错或覆盖。
func TestStatsSiteModelHourlySaveDBAccumulatesOnConflict(t *testing.T) {
	ctx := setupSiteOpTestDB(t)
	key := siteModelHourlyKey{Hour: 10, SiteAccountID: 1, GroupKey: "default", ModelName: "m"}
	save := func(date string, success int64, lastRequestAt int64) {
		row := &model.StatsSiteModelHourly{Hour: key.Hour, SiteAccountID: key.SiteAccountID, GroupKey: key.GroupKey, ModelName: key.ModelName, Date: date, LastRequestAt: lastRequestAt}
		row.RequestSuccess = success
		siteModelHourlyCacheLock.Lock()
		siteModelHourlyCache = map[siteModelHourlyKey]*model.StatsSiteModelHourly{key: row}
		siteModelHourlyCacheLock.Unlock()
		if err := StatsSiteModelHourlySaveDB(ctx); err != nil {
			t.Fatalf("StatsSiteModelHourlySaveDB failed: %v", err)
		}
	}
	save("20260101", 2, 200)
	save("20260102", 3, 100)

	var got model.StatsSiteModelHourly
	if err := dbpkg.GetDB().WithContext(ctx).First(&got).Error; err != nil {
		t.Fatalf("load row failed: %v", err)
	}
	if got.RequestSuccess != 5 || got.LastRequestAt != 200 || got.Date != "20260102" {
		t.Fatalf("expected success=5 last_request_at=200 date=20260102, got success=%d last_request_at=%d date=%s", got.RequestSuccess, got.LastRequestAt, got.Date)
	}
}

func TestSiteModelHourlyUpsertAssignmentsPerDialect(t *testing.T) {
	for dialect, want := range map[string][2]string{
		"sqlite":   {"EXCLUDED.date", "MAX(stats_site_model_hourlies.last_request_at, EXCLUDED.last_request_at)"},
		"postgres": {"EXCLUDED.date", "GREATEST(stats_site_model_hourlies.last_request_at, EXCLUDED.last_request_at)"},
		"mysql":    {"VALUES(date)", "GREATEST(stats_site_model_hourlies.last_request_at, VALUES(last_request_at))"},
	} {
		got := siteModelHourlyUpsertAssignments(dialect)
		for i, column := range []string{"date", "last_request_at"} {
			if sql := got[column].(clause.Expr).SQL; sql != want[i] {
				t.Fatalf("%s %s: got %q want %q", dialect, column, sql, want[i])
			}
		}
	}
}
