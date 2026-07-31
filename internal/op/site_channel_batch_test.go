package op

import (
	"testing"
	"time"

	dbpkg "github.com/bestruirui/octopus/internal/db"
	"github.com/bestruirui/octopus/internal/model"
)

func TestSiteChannelModelHourlyForAccountsMergesDBAndPending(t *testing.T) {
	ctx := setupSiteOpTestDB(t)

	currentHour := int(time.Now().Unix() / 3600)
	rows := []model.StatsSiteModelHourly{
		{Hour: currentHour - 1, SiteAccountID: 1, GroupKey: "default", ModelName: "gpt-4o", Date: "20240101", LastRequestAt: time.Now().Add(-time.Hour).Unix(), StatsMetrics: model.StatsMetrics{RequestSuccess: 2}},
		{Hour: currentHour, SiteAccountID: 2, GroupKey: "vip", ModelName: "claude", Date: "20240101", LastRequestAt: time.Now().Unix(), StatsMetrics: model.StatsMetrics{RequestFailed: 1}},
	}
	if err := dbpkg.GetDB().WithContext(ctx).Create(&rows).Error; err != nil {
		t.Fatalf("create stats rows failed: %v", err)
	}

	siteModelHourlyCacheLock.Lock()
	siteModelHourlyCache = map[siteModelHourlyKey]*model.StatsSiteModelHourly{
		{Hour: currentHour - 1, SiteAccountID: 1, GroupKey: "default", ModelName: "gpt-4o"}: {
			Hour: currentHour - 1, SiteAccountID: 1, GroupKey: "default", ModelName: "gpt-4o", Date: "20240101", LastRequestAt: time.Now().Unix(), StatsMetrics: model.StatsMetrics{RequestFailed: 3},
		},
	}
	siteModelHourlyCacheLock.Unlock()
	t.Cleanup(func() {
		siteModelHourlyCacheLock.Lock()
		siteModelHourlyCache = make(map[siteModelHourlyKey]*model.StatsSiteModelHourly)
		siteModelHourlyCacheLock.Unlock()
	})

	result, err := SiteChannelModelHourlyForAccounts(ctx, []int{1, 2})
	if err != nil {
		t.Fatalf("SiteChannelModelHourlyForAccounts failed: %v", err)
	}
	account1 := result[1]["default\x00gpt-4o"]
	if account1 == nil || account1.SuccessCount != 2 || account1.FailureCount != 3 || account1.LastRequestAt == nil || *account1.LastRequestAt <= 0 {
		t.Fatalf("unexpected account1 summary: %+v", account1)
	}
	account2 := result[2]["vip\x00claude"]
	if account2 == nil || account2.SuccessCount != 0 || account2.FailureCount != 1 || account2.LastRequestAt == nil || *account2.LastRequestAt <= 0 {
		t.Fatalf("unexpected account2 summary: %+v", account2)
	}
}

func TestStatsSiteModelHourlySaveDBRestoresRowsAfterFailure(t *testing.T) {
	ctx := setupSiteOpTestDB(t)
	currentHour := int(time.Now().Unix() / 3600)
	key := siteModelHourlyKey{
		Hour:          currentHour,
		SiteAccountID: 42,
		GroupKey:      "default",
		ModelName:     "gpt-4o",
	}
	row := &model.StatsSiteModelHourly{
		Hour:          key.Hour,
		SiteAccountID: key.SiteAccountID,
		GroupKey:      key.GroupKey,
		ModelName:     key.ModelName,
		Date:          time.Now().Format("20060102"),
		LastRequestAt: time.Now().Unix(),
		StatsMetrics: model.StatsMetrics{
			RequestSuccess: 3,
			RequestFailed:  1,
		},
	}

	siteModelHourlyCacheLock.Lock()
	siteModelHourlyCache = map[siteModelHourlyKey]*model.StatsSiteModelHourly{key: row}
	siteModelHourlyCacheLock.Unlock()
	t.Cleanup(func() {
		siteModelHourlyCacheLock.Lock()
		siteModelHourlyCache = make(map[siteModelHourlyKey]*model.StatsSiteModelHourly)
		siteModelHourlyCacheLock.Unlock()
	})

	dbConn := dbpkg.GetDB().WithContext(ctx)
	if err := dbConn.Migrator().DropTable(&model.StatsSiteModelHourly{}); err != nil {
		t.Fatalf("drop stats table: %v", err)
	}
	if err := StatsSiteModelHourlySaveDB(ctx); err == nil {
		t.Fatal("expected save to fail after dropping stats table")
	}

	siteModelHourlyCacheLock.Lock()
	restored := siteModelHourlyCache[key]
	siteModelHourlyCacheLock.Unlock()
	if restored == nil || restored.RequestSuccess != 3 || restored.RequestFailed != 1 {
		t.Fatalf("failed save did not restore pending stats: %+v", restored)
	}

	if err := dbConn.AutoMigrate(&model.StatsSiteModelHourly{}); err != nil {
		t.Fatalf("recreate stats table: %v", err)
	}
	if err := StatsSiteModelHourlySaveDB(ctx); err != nil {
		t.Fatalf("retry stats save: %v", err)
	}

	var saved model.StatsSiteModelHourly
	if err := dbConn.First(
		&saved,
		"hour = ? AND site_account_id = ? AND group_key = ? AND model_name = ?",
		key.Hour,
		key.SiteAccountID,
		key.GroupKey,
		key.ModelName,
	).Error; err != nil {
		t.Fatalf("load retried stats row: %v", err)
	}
	if saved.RequestSuccess != 3 || saved.RequestFailed != 1 {
		t.Fatalf("retried stats row mismatch: %+v", saved)
	}
}
