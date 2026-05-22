package op

import (
	"reflect"
	"strings"
	"testing"
	"time"

	dbpkg "github.com/bestruirui/octopus/internal/db"
	"github.com/bestruirui/octopus/internal/model"
)

// TestStatsSiteModelBackfillIgnoresHeavyContent 通过插入一批携带巨型
// request_content / response_content 的 relay_logs，验证回填依然能正确聚合
// 出小时桶。结构上 backfillLogRow 不再包含这两个字段，行为上这条用例
// 也保证 attempts JSON、legacy error 字段、setting 标记 等逻辑维持原状。
// 如果有人退化掉 Select 投影或扩张 backfillLogRow，本用例不会直接红，
// 但任何破坏聚合语义的修改都会失败。
func TestStatsSiteModelBackfillIgnoresHeavyContent(t *testing.T) {
	ctx := setupSiteOpTestDB(t)
	if err := settingRefreshCache(ctx); err != nil {
		t.Fatalf("settingRefreshCache failed: %v", err)
	}
	t.Cleanup(func() {
		siteModelHourlyCacheLock.Lock()
		siteModelHourlyCache = make(map[siteModelHourlyKey]*model.StatsSiteModelHourly)
		siteModelHourlyCacheLock.Unlock()
	})

	db := dbpkg.GetDB().WithContext(ctx)

	// FK constraints require parent rows: Site → SiteAccount → SiteChannelBinding.
	sites := []model.Site{
		{ID: 1, Name: "site-1", Platform: model.SitePlatformNewAPI, BaseURL: "https://a.example", Enabled: true},
	}
	if err := db.Create(&sites).Error; err != nil {
		t.Fatalf("create sites failed: %v", err)
	}
	accounts := []model.SiteAccount{
		{ID: 10, SiteID: 1, Name: "acct-default", CredentialType: model.SiteCredentialTypeAccessToken, AccessToken: "a", Enabled: true},
		{ID: 11, SiteID: 1, Name: "acct-vip", CredentialType: model.SiteCredentialTypeAccessToken, AccessToken: "b", Enabled: true},
	}
	if err := db.Create(&accounts).Error; err != nil {
		t.Fatalf("create accounts failed: %v", err)
	}
	bindings := []model.SiteChannelBinding{
		{SiteID: 1, SiteAccountID: 10, GroupKey: "default", ChannelID: 101},
		{SiteID: 1, SiteAccountID: 11, GroupKey: "vip", ChannelID: 202},
	}
	if err := db.Create(&bindings).Error; err != nil {
		t.Fatalf("create bindings failed: %v", err)
	}

	heavy := strings.Repeat("x", 64*1024) // 64KB per row, large enough to dwarf scalar fields
	now := time.Now().Unix()
	hour := int(now / 3600)
	logs := []model.RelayLog{
		{
			ID:               1,
			Time:             now,
			ChannelId:        101,
			ActualModelName:  "gpt-4o",
			RequestModelName: "gpt-4o",
			Error:            "",
			RequestContent:   heavy,
			ResponseContent:  heavy,
			Attempts: []model.ChannelAttempt{
				{ChannelID: 101, ModelName: "gpt-4o", Status: model.AttemptSuccess},
				{ChannelID: 202, ModelName: "claude", Status: model.AttemptFailed},
			},
		},
		{
			ID:               2,
			Time:             now,
			ChannelId:        202,
			ActualModelName:  "claude",
			RequestModelName: "claude",
			Error:            "timeout",
			RequestContent:   heavy,
			ResponseContent:  heavy,
			// no Attempts -> legacy code path: success := error == ""
		},
		{
			ID:               3,
			Time:             now,
			ChannelId:        999, // unbound channel; must be skipped silently
			ActualModelName:  "ghost",
			RequestModelName: "ghost",
			RequestContent:   heavy,
		},
	}
	if err := db.Create(&logs).Error; err != nil {
		t.Fatalf("create relay logs failed: %v", err)
	}

	StatsSiteModelBackfill(ctx)

	done, err := SettingGetBool(model.SettingKeyStatsSiteModelBackfilled)
	if err != nil {
		t.Fatalf("read backfilled flag failed: %v", err)
	}
	if !done {
		t.Fatalf("expected backfilled flag to be true after successful run")
	}

	var rows []model.StatsSiteModelHourly
	if err := db.Order("site_account_id ASC").Find(&rows).Error; err != nil {
		t.Fatalf("load aggregated rows failed: %v", err)
	}
	type key struct {
		account int
		group   string
		model   string
	}
	got := make(map[key]model.StatsSiteModelHourly, len(rows))
	for _, row := range rows {
		if row.Hour != hour {
			t.Fatalf("unexpected hour bucket %d (want %d)", row.Hour, hour)
		}
		got[key{row.SiteAccountID, row.GroupKey, row.ModelName}] = row
	}

	if entry, ok := got[key{10, "default", "gpt-4o"}]; !ok || entry.RequestSuccess != 1 || entry.RequestFailed != 0 {
		t.Fatalf("account=10 gpt-4o success bucket missing or wrong: %+v", entry)
	}
	if entry, ok := got[key{11, "vip", "claude"}]; !ok || entry.RequestSuccess != 0 || entry.RequestFailed != 2 {
		// One failure from the Attempts entry, one from the legacy Error="" check on log #2.
		t.Fatalf("account=11 claude failure bucket missing or wrong: %+v", entry)
	}
	for k := range got {
		if k.model == "ghost" {
			t.Fatalf("unbound channel was incorrectly aggregated: %+v", got[k])
		}
	}
}

// TestStatsSiteModelBackfillRowTypeSkipsContentFields 是一个结构层面的护栏：
// backfillLogRow 故意不包含 RequestContent / ResponseContent，本用例只是
// 把这一约束钉死，避免后续重构时不小心把大字段加回来。
func TestStatsSiteModelBackfillRowTypeSkipsContentFields(t *testing.T) {
	row := backfillLogRow{}
	// Typed reads keep the allowed field set load-bearing: removing or renaming
	// any of them breaks compilation here.
	_ = row.ID
	_ = row.Time
	_ = row.ChannelId
	_ = row.ActualModelName
	_ = row.RequestModelName
	_ = row.Error
	_ = row.Attempts

	// Reflective denylist: adding RequestContent / ResponseContent back would
	// make GORM SELECT the large content columns and reintroduce the OOM risk.
	rowType := reflect.TypeOf(row)
	for _, name := range []string{"RequestContent", "ResponseContent"} {
		if _, ok := rowType.FieldByName(name); ok {
			t.Fatalf("backfillLogRow must not contain %q: adding it forces GORM to SELECT the large content column and reintroduces the OOM risk", name)
		}
	}
}
