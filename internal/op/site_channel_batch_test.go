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
