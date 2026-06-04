package op

import (
	"context"
	"path/filepath"
	"testing"

	dbpkg "github.com/bestruirui/octopus/internal/db"
	"github.com/bestruirui/octopus/internal/model"
)

func setupSiteChannelSuspendTestDB(t *testing.T) context.Context {
	t.Helper()
	if dbpkg.GetDB() != nil {
		_ = dbpkg.Close()
	}
	dbPath := filepath.Join(t.TempDir(), "octopus-site-channel-suspend-test.db")
	if err := dbpkg.InitDB("sqlite", dbPath, false); err != nil {
		t.Fatalf("InitDB failed: %v", err)
	}
	t.Cleanup(func() { _ = dbpkg.Close() })
	return context.Background()
}

func createSiteChannelSuspendFixture(t *testing.T, ctx context.Context) (*model.Site, *model.SiteAccount) {
	t.Helper()
	site := &model.Site{Name: "Suspend Site", Platform: model.SitePlatformNewAPI, BaseURL: "https://example.com", Enabled: true}
	if err := SiteCreate(site, ctx); err != nil {
		t.Fatalf("SiteCreate failed: %v", err)
	}
	account := &model.SiteAccount{SiteID: site.ID, Name: "Primary", CredentialType: model.SiteCredentialTypeAccessToken, AccessToken: "token", Enabled: true, AutoSync: false, AutoCheckin: false}
	if err := SiteAccountCreate(account, ctx); err != nil {
		t.Fatalf("SiteAccountCreate failed: %v", err)
	}
	group := model.SiteUserGroup{
		SiteAccountID:           account.ID,
		GroupKey:                model.SiteDefaultGroupKey,
		Name:                    model.SiteDefaultGroupName,
		ProjectionSuspended:     true,
		ProjectionSuspendReason: "missing key",
		ModelSyncStatus:         model.SiteGroupModelSyncStatusMissingKey,
		ModelSyncMessage:        "missing key",
		ModelSyncFailureCount:   2,
	}
	if err := dbpkg.GetDB().WithContext(ctx).Create(&group).Error; err != nil {
		t.Fatalf("create suspended group failed: %v", err)
	}
	return site, account
}

func TestUpdateSiteSourceKeysRestoresSystemPausedProjection(t *testing.T) {
	ctx := setupSiteChannelSuspendTestDB(t)
	site, account := createSiteChannelSuspendFixture(t, ctx)
	modelRow := model.SiteModel{SiteAccountID: account.ID, GroupKey: model.SiteDefaultGroupKey, ModelName: "gpt-4o-restored", Source: "manual", RouteType: model.SiteModelRouteTypeOpenAIChat, RouteSource: model.SiteModelRouteSourceManualOverride, ManualOverride: true}
	if err := dbpkg.GetDB().WithContext(ctx).Create(&modelRow).Error; err != nil {
		t.Fatalf("create model failed: %v", err)
	}

	req := &model.SiteSourceKeyUpdateRequest{
		GroupKey: model.SiteDefaultGroupKey,
		KeysToAdd: []model.SiteSourceKeyAddRequest{{
			Enabled: true,
			Token:   "key-restored",
			Name:    "restored",
		}},
	}
	if err := UpdateSiteSourceKeys(site.ID, account.ID, req, ctx); err != nil {
		t.Fatalf("UpdateSiteSourceKeys failed: %v", err)
	}

	var group model.SiteUserGroup
	if err := dbpkg.GetDB().WithContext(ctx).Where("site_account_id = ? AND group_key = ?", account.ID, model.SiteDefaultGroupKey).First(&group).Error; err != nil {
		t.Fatalf("query group failed: %v", err)
	}
	if group.ProjectionSuspended {
		t.Fatalf("expected source key update to restore suspended projection")
	}
	if group.ModelSyncStatus != model.SiteGroupModelSyncStatusIdle {
		t.Fatalf("expected restored sync status idle, got %q", group.ModelSyncStatus)
	}
}

func TestSiteManualModelsAddRestoresSystemPausedProjection(t *testing.T) {
	ctx := setupSiteChannelSuspendTestDB(t)
	site, account := createSiteChannelSuspendFixture(t, ctx)
	token := model.SiteToken{SiteAccountID: account.ID, Name: "ready", Token: "key-ready", GroupKey: model.SiteDefaultGroupKey, GroupName: model.SiteDefaultGroupName, Enabled: true, ValueStatus: model.SiteTokenValueStatusReady}
	if err := dbpkg.GetDB().WithContext(ctx).Create(&token).Error; err != nil {
		t.Fatalf("create ready token failed: %v", err)
	}

	req := &model.SiteManualModelAddRequest{
		GroupKey: model.SiteDefaultGroupKey,
		Models: []model.SiteManualModelAddEntry{{
			ModelName: "gpt-4o-manual",
			RouteType: model.SiteModelRouteTypeOpenAIChat,
		}},
	}
	if err := SiteManualModelsAdd(site.ID, account.ID, req, ctx); err != nil {
		t.Fatalf("SiteManualModelsAdd failed: %v", err)
	}

	var group model.SiteUserGroup
	if err := dbpkg.GetDB().WithContext(ctx).Where("site_account_id = ? AND group_key = ?", account.ID, model.SiteDefaultGroupKey).First(&group).Error; err != nil {
		t.Fatalf("query group failed: %v", err)
	}
	if group.ProjectionSuspended {
		t.Fatalf("expected manual model add to restore suspended projection")
	}
	if group.ModelSyncStatus != model.SiteGroupModelSyncStatusIdle {
		t.Fatalf("expected restored sync status idle, got %q", group.ModelSyncStatus)
	}
}
