package op

import (
	"context"
	"fmt"
	"path/filepath"
	"testing"

	dbpkg "github.com/bestruirui/octopus/internal/db"
	"github.com/bestruirui/octopus/internal/model"
)

func setupBackupTestDB(t *testing.T) context.Context {
	t.Helper()

	if dbpkg.GetDB() != nil {
		_ = dbpkg.Close()
	}

	dbPath := filepath.Join(t.TempDir(), "octopus-backup-test.db")
	if err := dbpkg.InitDB("sqlite", dbPath, false); err != nil {
		t.Fatalf("InitDB failed: %v", err)
	}
	t.Cleanup(func() {
		_ = dbpkg.Close()
	})

	return context.Background()
}

func TestDBImportPreservesAllAccountsOnCleanDB(t *testing.T) {
	ctx := setupBackupTestDB(t)

	dump := buildTestDump()
	result, err := DBImportIncremental(ctx, dump)
	if err != nil {
		t.Fatalf("DBImportIncremental failed: %v", err)
	}

	if result.RowsAffected["sites"] != 1 {
		t.Fatalf("expected 1 site created, got %d", result.RowsAffected["sites"])
	}
	if result.RowsAffected["site_accounts"] != 3 {
		t.Fatalf("expected 3 site accounts created, got %d", result.RowsAffected["site_accounts"])
	}

	site, err := SiteGet(1, ctx)
	if err != nil {
		// Site might have a different ID after import; query by platform+url
		var sites []model.Site
		if qerr := dbpkg.GetDB().Where("platform = ? AND base_url = ?", "new-api", "https://example.com").Find(&sites).Error; qerr != nil {
			t.Fatalf("query sites failed: %v", qerr)
		}
		if len(sites) != 1 {
			t.Fatalf("expected 1 site, got %d", len(sites))
		}
		site, err = SiteGet(sites[0].ID, ctx)
		if err != nil {
			t.Fatalf("SiteGet failed: %v", err)
		}
	}
	if len(site.Accounts) != 3 {
		t.Fatalf("expected site to have 3 accounts, got %d", len(site.Accounts))
	}
}

func TestDBImportWithIDCollisionPreservesAllAccounts(t *testing.T) {
	ctx := setupBackupTestDB(t)

	// Create pre-existing data that will cause ID collisions
	preexistingSite := &model.Site{
		Name:     "other-site",
		Platform: model.SitePlatformOneAPI,
		BaseURL:  "https://other.com",
		Enabled:  true,
	}
	if err := SiteCreate(preexistingSite, ctx); err != nil {
		t.Fatalf("create pre-existing site failed: %v", err)
	}
	preexistingAccount := &model.SiteAccount{
		SiteID:         preexistingSite.ID,
		Name:           "other-account",
		CredentialType: model.SiteCredentialTypeAPIKey,
		APIKey:         "sk-other",
		Enabled:        true,
		AutoSync:       true,
	}
	if err := SiteAccountCreate(preexistingAccount, ctx); err != nil {
		t.Fatalf("create pre-existing account failed: %v", err)
	}

	// Now import a dump that has records with IDs that overlap
	dump := buildTestDump()
	result, err := DBImportIncremental(ctx, dump)
	if err != nil {
		t.Fatalf("DBImportIncremental failed: %v", err)
	}

	// All 3 accounts from the dump should be imported
	if result.RowsAffected["site_accounts"] != 3 {
		t.Fatalf("expected 3 site accounts created, got %d", result.RowsAffected["site_accounts"])
	}

	// The pre-existing data should still be intact
	var totalAccounts int64
	if err := dbpkg.GetDB().Model(&model.SiteAccount{}).Count(&totalAccounts).Error; err != nil {
		t.Fatalf("count accounts failed: %v", err)
	}
	if totalAccounts != 4 { // 1 pre-existing + 3 imported
		t.Fatalf("expected 4 total accounts, got %d", totalAccounts)
	}

	// Verify the imported site has all 3 accounts
	var importedSite model.Site
	if err := dbpkg.GetDB().Where("platform = ? AND base_url = ?", "new-api", "https://example.com").First(&importedSite).Error; err != nil {
		t.Fatalf("query imported site failed: %v", err)
	}
	var importedAccountCount int64
	if err := dbpkg.GetDB().Model(&model.SiteAccount{}).Where("site_id = ?", importedSite.ID).Count(&importedAccountCount).Error; err != nil {
		t.Fatalf("count imported accounts failed: %v", err)
	}
	if importedAccountCount != 3 {
		t.Fatalf("expected imported site to have 3 accounts, got %d", importedAccountCount)
	}
}

func TestDBImportDeduplicatesOnSecondImport(t *testing.T) {
	ctx := setupBackupTestDB(t)

	dump := buildTestDump()

	// First import
	if _, err := DBImportIncremental(ctx, dump); err != nil {
		t.Fatalf("first DBImportIncremental failed: %v", err)
	}

	// Second import of the same data
	dump2 := buildTestDump()
	result, err := DBImportIncremental(ctx, dump2)
	if err != nil {
		t.Fatalf("second DBImportIncremental failed: %v", err)
	}

	// Nothing new should be created (all deduped)
	if result.RowsAffected["sites"] != 0 {
		t.Fatalf("expected 0 new sites on second import, got %d", result.RowsAffected["sites"])
	}
	if result.RowsAffected["site_accounts"] != 0 {
		t.Fatalf("expected 0 new accounts on second import, got %d", result.RowsAffected["site_accounts"])
	}
	if result.RowsAffected["channels"] != 0 {
		t.Fatalf("expected 0 new channels on second import, got %d", result.RowsAffected["channels"])
	}

	// Total counts should remain the same
	var siteCount, accountCount, channelCount int64
	dbpkg.GetDB().Model(&model.Site{}).Count(&siteCount)
	dbpkg.GetDB().Model(&model.SiteAccount{}).Count(&accountCount)
	dbpkg.GetDB().Model(&model.Channel{}).Count(&channelCount)

	if siteCount != 1 {
		t.Fatalf("expected 1 site after double import, got %d", siteCount)
	}
	if accountCount != 3 {
		t.Fatalf("expected 3 accounts after double import, got %d", accountCount)
	}
	if channelCount != 1 {
		t.Fatalf("expected 1 channel after double import, got %d", channelCount)
	}
}

func TestDBExportThenImportRoundtrip(t *testing.T) {
	ctx := setupBackupTestDB(t)

	// Create test data
	site := &model.Site{
		Name:     "roundtrip-site",
		Platform: model.SitePlatformNewAPI,
		BaseURL:  "https://roundtrip.example.com",
		Enabled:  true,
	}
	if err := SiteCreate(site, ctx); err != nil {
		t.Fatalf("SiteCreate failed: %v", err)
	}
	for i := 0; i < 5; i++ {
		account := &model.SiteAccount{
			SiteID:         site.ID,
			Name:           mustSprintf("account-%d", i),
			CredentialType: model.SiteCredentialTypeAPIKey,
			APIKey:         mustSprintf("sk-key-%d", i),
			Enabled:        true,
			AutoSync:       true,
		}
		if err := SiteAccountCreate(account, ctx); err != nil {
			t.Fatalf("SiteAccountCreate failed: %v", err)
		}
	}

	// Export
	dump, err := DBExportAll(ctx, false, false)
	if err != nil {
		t.Fatalf("DBExportAll failed: %v", err)
	}

	// Verify export contains all accounts
	if len(dump.SiteAccounts) != 5 {
		t.Fatalf("expected 5 accounts in export, got %d", len(dump.SiteAccounts))
	}

	// Close and re-create a fresh DB
	_ = dbpkg.Close()
	freshDBPath := filepath.Join(t.TempDir(), "octopus-fresh.db")
	if err := dbpkg.InitDB("sqlite", freshDBPath, false); err != nil {
		t.Fatalf("InitDB for fresh DB failed: %v", err)
	}

	// Import to fresh DB
	result, err := DBImportIncremental(ctx, dump)
	if err != nil {
		t.Fatalf("DBImportIncremental to fresh DB failed: %v", err)
	}
	if result.RowsAffected["sites"] != 1 {
		t.Fatalf("expected 1 site imported, got %d", result.RowsAffected["sites"])
	}
	if result.RowsAffected["site_accounts"] != 5 {
		t.Fatalf("expected 5 accounts imported, got %d", result.RowsAffected["site_accounts"])
	}

	// Verify all accounts are present
	var freshSite model.Site
	if err := dbpkg.GetDB().Where("platform = ? AND base_url = ?", "new-api", "https://roundtrip.example.com").First(&freshSite).Error; err != nil {
		t.Fatalf("query imported site failed: %v", err)
	}
	var accountCount int64
	if err := dbpkg.GetDB().Model(&model.SiteAccount{}).Where("site_id = ?", freshSite.ID).Count(&accountCount).Error; err != nil {
		t.Fatalf("count accounts failed: %v", err)
	}
	if accountCount != 5 {
		t.Fatalf("expected 5 accounts for imported site, got %d", accountCount)
	}
}

func buildTestDump() *model.DBDump {
	return &model.DBDump{
		Version:    1,
		IncludeLogs: false,
		IncludeStats: false,
		Channels: []model.Channel{
			{ID: 1, Name: "test-channel", Enabled: true},
		},
		ChannelKeys: []model.ChannelKey{
			{ID: 1, ChannelID: 1, Enabled: true, ChannelKey: "sk-chan-1"},
		},
		Sites: []model.Site{
			{ID: 1, Name: "test-site", Platform: model.SitePlatformNewAPI, BaseURL: "https://example.com", Enabled: true},
		},
		SiteAccounts: []model.SiteAccount{
			{ID: 1, SiteID: 1, Name: "account-1", CredentialType: model.SiteCredentialTypeAPIKey, APIKey: "sk-1", Enabled: true, AutoSync: true},
			{ID: 2, SiteID: 1, Name: "account-2", CredentialType: model.SiteCredentialTypeAPIKey, APIKey: "sk-2", Enabled: true, AutoSync: true},
			{ID: 3, SiteID: 1, Name: "account-3", CredentialType: model.SiteCredentialTypeAPIKey, APIKey: "sk-3", Enabled: true, AutoSync: true},
		},
		Groups: []model.Group{
			{ID: 1, Name: "test-group", Mode: 0},
		},
		GroupItems: []model.GroupItem{
			{ID: 1, GroupID: 1, ChannelID: 1, ModelName: "gpt-4", Priority: 1, Weight: 1},
		},
	}
}

func mustSprintf(format string, args ...any) string {
	return fmt.Sprintf(format, args...)
}
