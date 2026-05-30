package op

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"github.com/bestruirui/octopus/internal/apperror"
	dbpkg "github.com/bestruirui/octopus/internal/db"
	"github.com/bestruirui/octopus/internal/model"
)

func setupSiteOpTestDB(t *testing.T) context.Context {
	t.Helper()

	if dbpkg.GetDB() != nil {
		_ = dbpkg.Close()
	}

	dbPath := filepath.Join(t.TempDir(), "octopus-test.db")
	if err := dbpkg.InitDB("sqlite", dbPath, false); err != nil {
		t.Fatalf("InitDB failed: %v", err)
	}
	t.Cleanup(func() {
		_ = dbpkg.Close()
	})

	return context.Background()
}

func createSiteOpTestSiteAccount(t *testing.T, ctx context.Context, siteName, accountName string) (*model.Site, *model.SiteAccount) {
	t.Helper()

	site := &model.Site{
		Name:     siteName,
		Platform: model.SitePlatformNewAPI,
		BaseURL:  "https://example.com",
		Enabled:  true,
	}
	if err := SiteCreate(site, ctx); err != nil {
		t.Fatalf("SiteCreate failed: %v", err)
	}

	account := &model.SiteAccount{
		SiteID:         site.ID,
		Name:           accountName,
		CredentialType: model.SiteCredentialTypeAccessToken,
		AccessToken:    "token",
		Enabled:        true,
	}
	if err := SiteAccountCreate(account, ctx); err != nil {
		t.Fatalf("SiteAccountCreate failed: %v", err)
	}
	return site, account
}

func createLegacySitePricesRow(t *testing.T, ctx context.Context, accountID int) {
	t.Helper()
	if err := dbpkg.GetDB().WithContext(ctx).Exec(`CREATE TABLE site_prices (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		site_account_id INTEGER NOT NULL,
		group_key TEXT NOT NULL DEFAULT 'default',
		model_name TEXT NOT NULL,
		input_price REAL,
		CONSTRAINT fk_site_accounts_prices FOREIGN KEY (site_account_id) REFERENCES site_accounts(id)
	)`).Error; err != nil {
		t.Fatalf("create legacy site_prices table failed: %v", err)
	}
	if err := dbpkg.GetDB().WithContext(ctx).Exec("INSERT INTO site_prices (site_account_id, group_key, model_name, input_price) VALUES (?, ?, ?, ?)", accountID, model.SiteDefaultGroupKey, "gpt-4o-mini", 1.23).Error; err != nil {
		t.Fatalf("insert legacy site_prices row failed: %v", err)
	}
}

func assertLegacySitePricesCount(t *testing.T, ctx context.Context, accountID int, want int64) {
	t.Helper()
	var count int64
	if err := dbpkg.GetDB().WithContext(ctx).Table("site_prices").Where("site_account_id = ?", accountID).Count(&count).Error; err != nil {
		t.Fatalf("count legacy site_prices failed: %v", err)
	}
	if count != want {
		t.Fatalf("expected legacy site_prices count %d, got %d", want, count)
	}
}

func TestSiteDelDeletesLegacySitePrices(t *testing.T) {
	ctx := setupSiteOpTestDB(t)
	_, account := createSiteOpTestSiteAccount(t, ctx, "legacy-price-site", "legacy-price-account")
	createLegacySitePricesRow(t, ctx, account.ID)

	if err := SiteDel(account.SiteID, ctx); err != nil {
		t.Fatalf("SiteDel failed: %v", err)
	}
	assertLegacySitePricesCount(t, ctx, account.ID, 0)
}

func TestSiteAccountDelDeletesLegacySitePrices(t *testing.T) {
	ctx := setupSiteOpTestDB(t)
	_, account := createSiteOpTestSiteAccount(t, ctx, "legacy-account-price-site", "legacy-account-price-account")
	createLegacySitePricesRow(t, ctx, account.ID)

	if err := SiteAccountDel(account.ID, ctx); err != nil {
		t.Fatalf("SiteAccountDel failed: %v", err)
	}
	assertLegacySitePricesCount(t, ctx, account.ID, 0)
}

func TestSiteCreateAndAccountCreatePersistExplicitFalseValues(t *testing.T) {
	ctx := setupSiteOpTestDB(t)

	var site model.Site
	if err := json.Unmarshal([]byte(`{"name":"disabled-site","platform":"new-api","base_url":"https://example.com","enabled":false}`), &site); err != nil {
		t.Fatalf("json.Unmarshal Site failed: %v", err)
	}
	if !site.EnabledSet {
		t.Fatalf("expected enabled presence flag to be set")
	}
	if err := SiteCreate(&site, ctx); err != nil {
		t.Fatalf("SiteCreate failed: %v", err)
	}

	reloadedSite, err := SiteGet(site.ID, ctx)
	if err != nil {
		t.Fatalf("SiteGet failed: %v", err)
	}
	if reloadedSite.Enabled {
		t.Fatalf("expected explicitly disabled site to stay disabled")
	}

	var account model.SiteAccount
	if err := json.Unmarshal([]byte(`{"site_id":`+fmt.Sprint(site.ID)+`,"name":"manual-account","credential_type":"access_token","access_token":"token","enabled":false,"auto_sync":false,"auto_checkin":false}`), &account); err != nil {
		t.Fatalf("json.Unmarshal SiteAccount failed: %v", err)
	}
	if !account.EnabledSet || !account.AutoSyncSet || !account.AutoCheckinSet {
		t.Fatalf("expected account boolean presence flags to be set")
	}
	if err := SiteAccountCreate(&account, ctx); err != nil {
		t.Fatalf("SiteAccountCreate failed: %v", err)
	}

	reloadedAccount, err := SiteAccountGet(account.ID, ctx)
	if err != nil {
		t.Fatalf("SiteAccountGet failed: %v", err)
	}
	if reloadedAccount.Enabled || reloadedAccount.AutoSync || reloadedAccount.AutoCheckin {
		t.Fatalf("expected explicit false flags to persist, got enabled=%v auto_sync=%v auto_checkin=%v", reloadedAccount.Enabled, reloadedAccount.AutoSync, reloadedAccount.AutoCheckin)
	}
}

func TestSiteUpdateCanClearNullableFields(t *testing.T) {
	ctx := setupSiteOpTestDB(t)

	checkinURL := "https://example.com/signin"
	proxyConfigID := 123
	site := &model.Site{
		Name:               "checkin-site",
		Platform:           model.SitePlatformNewAPI,
		BaseURL:            "https://example.com",
		Enabled:            true,
		ExternalCheckinURL: &checkinURL,
	}
	if err := SiteCreate(site, ctx); err != nil {
		t.Fatalf("SiteCreate failed: %v", err)
	}

	// Simulate an existing pool config without requiring a proxy config row, then switch away from pool and clear it.
	if err := dbpkg.GetDB().Model(&model.Site{}).Where("id = ?", site.ID).Updates(map[string]any{
		"proxy_mode":      model.ProxyUsageModePool,
		"proxy_config_id": proxyConfigID,
	}).Error; err != nil {
		t.Fatalf("seed proxy config failed: %v", err)
	}

	var req model.SiteUpdateRequest
	if err := json.Unmarshal([]byte(`{"id":`+fmt.Sprint(site.ID)+`,"external_checkin_url":null,"proxy_mode":"direct","proxy_config_id":null}`), &req); err != nil {
		t.Fatalf("json.Unmarshal SiteUpdateRequest failed: %v", err)
	}
	if !req.ExternalCheckinSet || !req.ProxyConfigIDSet {
		t.Fatalf("expected nullable field presence flags to be set")
	}

	updated, err := SiteUpdate(&req, ctx)
	if err != nil {
		t.Fatalf("SiteUpdate failed: %v", err)
	}
	if updated.ExternalCheckinURL != nil {
		t.Fatalf("expected external checkin url to be cleared, got %#v", *updated.ExternalCheckinURL)
	}
	if updated.ProxyConfigID != nil {
		t.Fatalf("expected proxy config id to be cleared, got %#v", *updated.ProxyConfigID)
	}
}

func TestSiteAccountUpdateCanClearNullableFields(t *testing.T) {
	ctx := setupSiteOpTestDB(t)

	site := &model.Site{
		Name:     "account-nullable-site",
		Platform: model.SitePlatformNewAPI,
		BaseURL:  "https://example.com",
		Enabled:  true,
	}
	if err := SiteCreate(site, ctx); err != nil {
		t.Fatalf("SiteCreate failed: %v", err)
	}

	platformUserID := 456
	account := &model.SiteAccount{
		SiteID:         site.ID,
		Name:           "nullable-account",
		CredentialType: model.SiteCredentialTypeAccessToken,
		AccessToken:    "token",
		PlatformUserID: &platformUserID,
		Enabled:        true,
		AutoSync:       true,
		AutoCheckin:    true,
	}
	if err := SiteAccountCreate(account, ctx); err != nil {
		t.Fatalf("SiteAccountCreate failed: %v", err)
	}

	proxyConfigID := 123
	if err := dbpkg.GetDB().Model(&model.SiteAccount{}).Where("id = ?", account.ID).Updates(map[string]any{
		"proxy_mode":      model.ProxyUsageModePool,
		"proxy_config_id": proxyConfigID,
	}).Error; err != nil {
		t.Fatalf("seed account proxy config failed: %v", err)
	}

	var req model.SiteAccountUpdateRequest
	if err := json.Unmarshal([]byte(`{"id":`+fmt.Sprint(account.ID)+`,"platform_user_id":null,"proxy_mode":"inherit","proxy_config_id":null}`), &req); err != nil {
		t.Fatalf("json.Unmarshal SiteAccountUpdateRequest failed: %v", err)
	}
	if !req.PlatformUserIDSet || !req.ProxyConfigIDSet {
		t.Fatalf("expected nullable field presence flags to be set")
	}

	updated, err := SiteAccountUpdate(&req, ctx)
	if err != nil {
		t.Fatalf("SiteAccountUpdate failed: %v", err)
	}
	if updated.PlatformUserID != nil {
		t.Fatalf("expected platform user id to be cleared, got %#v", *updated.PlatformUserID)
	}
	if updated.ProxyConfigID != nil {
		t.Fatalf("expected proxy config id to be cleared, got %#v", *updated.ProxyConfigID)
	}
}

func TestSiteUpdateRejectsInvalidMergedSite(t *testing.T) {
	ctx := setupSiteOpTestDB(t)

	site := &model.Site{
		Name:     "demo-site",
		Platform: model.SitePlatformNewAPI,
		BaseURL:  "https://example.com",
		Enabled:  true,
	}
	if err := SiteCreate(site, ctx); err != nil {
		t.Fatalf("SiteCreate failed: %v", err)
	}

	invalidBaseURL := "not-a-valid-url"
	if _, err := SiteUpdate(&model.SiteUpdateRequest{
		ID:      site.ID,
		BaseURL: &invalidBaseURL,
	}, ctx); err == nil {
		t.Fatalf("expected SiteUpdate to reject invalid merged site")
	}

	reloaded, err := SiteGet(site.ID, ctx)
	if err != nil {
		t.Fatalf("SiteGet failed: %v", err)
	}
	if reloaded.BaseURL != "https://example.com" {
		t.Fatalf("expected original base URL to remain unchanged, got %q", reloaded.BaseURL)
	}
}

func TestSiteAccountUpdateRejectsInvalidMergedCredentials(t *testing.T) {
	ctx := setupSiteOpTestDB(t)

	site := &model.Site{
		Name:     "demo-site",
		Platform: model.SitePlatformNewAPI,
		BaseURL:  "https://example.com",
		Enabled:  true,
	}
	if err := SiteCreate(site, ctx); err != nil {
		t.Fatalf("SiteCreate failed: %v", err)
	}

	account := &model.SiteAccount{
		SiteID:         site.ID,
		Name:           "demo-account",
		CredentialType: model.SiteCredentialTypeUsernamePassword,
		Username:       "user",
		Password:       "pass",
		Enabled:        true,
		AutoSync:       true,
		AutoCheckin:    true,
	}
	if err := SiteAccountCreate(account, ctx); err != nil {
		t.Fatalf("SiteAccountCreate failed: %v", err)
	}

	newCredentialType := model.SiteCredentialTypeAccessToken
	if _, err := SiteAccountUpdate(&model.SiteAccountUpdateRequest{
		ID:             account.ID,
		CredentialType: &newCredentialType,
	}, ctx); err == nil {
		t.Fatalf("expected SiteAccountUpdate to reject invalid merged credentials")
	}

	reloaded, err := SiteAccountGet(account.ID, ctx)
	if err != nil {
		t.Fatalf("SiteAccountGet failed: %v", err)
	}
	if reloaded.CredentialType != model.SiteCredentialTypeUsernamePassword {
		t.Fatalf("expected credential type to remain username_password, got %q", reloaded.CredentialType)
	}
}

func TestSiteImportAllAPIHubImportsAndUpdatesAccounts(t *testing.T) {
	ctx := setupSiteOpTestDB(t)

	result, syncAccountIDs, err := SiteImportAllAPIHub(ctx, mustJSONMarshal(t, buildAllAPIHubImportPayload("managed-user")))
	if err != nil {
		t.Fatalf("SiteImportAllAPIHub failed: %v", err)
	}

	if result.CreatedSites != 7 {
		t.Fatalf("expected 7 created sites, got %d", result.CreatedSites)
	}
	if result.ReusedSites != 0 {
		t.Fatalf("expected 0 reused sites on first import, got %d", result.ReusedSites)
	}
	if result.CreatedAccounts != 8 {
		t.Fatalf("expected 8 created accounts, got %d", result.CreatedAccounts)
	}
	if result.UpdatedAccounts != 0 {
		t.Fatalf("expected 0 updated accounts on first import, got %d", result.UpdatedAccounts)
	}
	if result.SkippedAccounts != 2 {
		t.Fatalf("expected 2 skipped accounts, got %d", result.SkippedAccounts)
	}
	if result.ScheduledSyncAccounts != 8 {
		t.Fatalf("expected 8 scheduled sync accounts, got %d", result.ScheduledSyncAccounts)
	}
	if len(syncAccountIDs) != 8 {
		t.Fatalf("expected 8 sync account IDs, got %d", len(syncAccountIDs))
	}
	if len(result.Warnings) != 2 {
		t.Fatalf("expected 2 warnings, got %d", len(result.Warnings))
	}
	if !containsAll(result.Warnings, "skipped-none-account", "skipped-empty-account") {
		t.Fatalf("expected warnings to mention skipped account IDs, got %#v", result.Warnings)
	}

	var siteCount int64
	if err := dbpkg.GetDB().Model(&model.Site{}).Count(&siteCount).Error; err != nil {
		t.Fatalf("count sites failed: %v", err)
	}
	if siteCount != 7 {
		t.Fatalf("expected 7 sites in database, got %d", siteCount)
	}

	var accountCount int64
	if err := dbpkg.GetDB().Model(&model.SiteAccount{}).Count(&accountCount).Error; err != nil {
		t.Fatalf("count site accounts failed: %v", err)
	}
	if accountCount != 8 {
		t.Fatalf("expected 8 site accounts in database, got %d", accountCount)
	}

	assertImportedAccount(t, "managed-user", func(account model.SiteAccount) {
		if account.CredentialType != model.SiteCredentialTypeAccessToken {
			t.Fatalf("expected managed account credential type %q, got %q", model.SiteCredentialTypeAccessToken, account.CredentialType)
		}
		if account.AccessToken != "managed-session-token" {
			t.Fatalf("expected managed access token to be imported, got %q", account.AccessToken)
		}
		if account.PlatformUserID == nil || *account.PlatformUserID != 7788 {
			t.Fatalf("expected managed platform user id 7788, got %#v", account.PlatformUserID)
		}
		if !account.AutoCheckin {
			t.Fatalf("expected managed account auto checkin to be enabled")
		}
	})

	assertImportedAccount(t, "cookie-user", func(account model.SiteAccount) {
		if account.CredentialType != model.SiteCredentialTypeAccessToken {
			t.Fatalf("expected cookie account credential type %q, got %q", model.SiteCredentialTypeAccessToken, account.CredentialType)
		}
		if account.AccessToken != "sid=cookie-session" {
			t.Fatalf("expected cookie session to be stored as access token, got %q", account.AccessToken)
		}
		if account.AutoCheckin {
			t.Fatalf("expected cookie account auto checkin to stay disabled")
		}
	})

	assertImportedAccount(t, "openai-account", func(account model.SiteAccount) {
		if account.CredentialType != model.SiteCredentialTypeAPIKey {
			t.Fatalf("expected direct OpenAI account credential type %q, got %q", model.SiteCredentialTypeAPIKey, account.CredentialType)
		}
		if account.APIKey != "sk-openai-account" {
			t.Fatalf("expected direct OpenAI api key to be imported, got %q", account.APIKey)
		}
		if account.AutoCheckin {
			t.Fatalf("expected direct OpenAI account auto checkin to be disabled")
		}
	})

	var openAISiteCount int64
	if err := dbpkg.GetDB().Model(&model.Site{}).Where("platform = ? AND base_url = ?", model.SitePlatformOpenAI, "https://api.openai.com").Count(&openAISiteCount).Error; err != nil {
		t.Fatalf("count openai sites failed: %v", err)
	}
	if openAISiteCount != 1 {
		t.Fatalf("expected one normalized OpenAI site, got %d", openAISiteCount)
	}

	var compatSite model.Site
	if err := dbpkg.GetDB().Where("platform = ? AND base_url = ?", model.SitePlatformOpenAI, "https://compat.example.com").First(&compatSite).Error; err != nil {
		t.Fatalf("query compat site failed: %v", err)
	}

	result, syncAccountIDs, err = SiteImportAllAPIHub(ctx, mustJSONMarshal(t, buildAllAPIHubImportPayload("managed-user-renamed")))
	if err != nil {
		t.Fatalf("second SiteImportAllAPIHub failed: %v", err)
	}

	if result.CreatedSites != 0 {
		t.Fatalf("expected 0 created sites on second import, got %d", result.CreatedSites)
	}
	if result.ReusedSites != 7 {
		t.Fatalf("expected 7 reused sites on second import, got %d", result.ReusedSites)
	}
	if result.CreatedAccounts != 0 {
		t.Fatalf("expected 0 created accounts on second import, got %d", result.CreatedAccounts)
	}
	if result.UpdatedAccounts != 8 {
		t.Fatalf("expected 8 updated accounts on second import, got %d", result.UpdatedAccounts)
	}
	if result.SkippedAccounts != 2 {
		t.Fatalf("expected 2 skipped accounts on second import, got %d", result.SkippedAccounts)
	}
	if result.ScheduledSyncAccounts != 8 {
		t.Fatalf("expected 8 scheduled sync accounts on second import, got %d", result.ScheduledSyncAccounts)
	}
	if len(syncAccountIDs) != 8 {
		t.Fatalf("expected 8 sync account IDs on second import, got %d", len(syncAccountIDs))
	}

	if err := dbpkg.GetDB().Model(&model.SiteAccount{}).Count(&accountCount).Error; err != nil {
		t.Fatalf("count site accounts after second import failed: %v", err)
	}
	if accountCount != 8 {
		t.Fatalf("expected 8 site accounts after second import, got %d", accountCount)
	}

	assertImportedAccount(t, "managed-user-renamed", func(account model.SiteAccount) {
		if account.AccessToken != "managed-session-token" {
			t.Fatalf("expected managed token to remain stable after reimport, got %q", account.AccessToken)
		}
	})
}

func TestSiteImportMetAPIImportsSiteBasics(t *testing.T) {
	ctx := setupSiteOpTestDB(t)

	result, err := SiteImportMetAPI(ctx, mustJSONMarshal(t, buildMetAPIImportPayload("metapi-user")))
	if err != nil {
		t.Fatalf("SiteImportMetAPI failed: %v", err)
	}

	if result.CreatedSites != 2 {
		t.Fatalf("expected 2 created sites, got %d", result.CreatedSites)
	}
	if result.CreatedAccounts != 2 {
		t.Fatalf("expected 2 created accounts, got %d", result.CreatedAccounts)
	}
	if result.UpdatedAccounts != 0 {
		t.Fatalf("expected 0 updated accounts, got %d", result.UpdatedAccounts)
	}
	if result.ImportedTokens != 3 {
		t.Fatalf("expected 3 imported tokens, got %d", result.ImportedTokens)
	}
	if result.ImportedGroups != 3 {
		t.Fatalf("expected 3 imported groups, got %d", result.ImportedGroups)
	}
	if result.ImportedModels != 3 {
		t.Fatalf("expected 3 imported models, got %d", result.ImportedModels)
	}
	if result.DisabledModels != 1 {
		t.Fatalf("expected 1 disabled model, got %d", result.DisabledModels)
	}
	if len(result.Warnings) != 2 {
		t.Fatalf("expected warnings for skipped routes and downstream keys, got %#v", result.Warnings)
	}

	var managed model.SiteAccount
	if err := dbpkg.GetDB().Where("name = ?", "metapi-user").First(&managed).Error; err != nil {
		t.Fatalf("query managed account failed: %v", err)
	}
	if managed.CredentialType != model.SiteCredentialTypeAccessToken {
		t.Fatalf("expected managed credential type access_token, got %q", managed.CredentialType)
	}
	if managed.AccessToken != "metapi-session-token" {
		t.Fatalf("expected metapi session token, got %q", managed.AccessToken)
	}
	if managed.APIKey != "sk-metapi-default" {
		t.Fatalf("expected metapi api token fallback, got %q", managed.APIKey)
	}
	if managed.PlatformUserID == nil || *managed.PlatformUserID != 456 {
		t.Fatalf("expected platform user id 456, got %#v", managed.PlatformUserID)
	}
	if managed.AccountProxy != nil {
		t.Fatalf("expected legacy account proxy to stay empty, got %#v", managed.AccountProxy)
	}
	if managed.ProxyMode != model.ProxyUsageModePool || managed.ProxyConfigID == nil {
		t.Fatalf("expected account proxy to be resolved through proxy pool, got mode=%q id=%#v", managed.ProxyMode, managed.ProxyConfigID)
	}

	var tokenCount int64
	if err := dbpkg.GetDB().Model(&model.SiteToken{}).Where("site_account_id = ?", managed.ID).Count(&tokenCount).Error; err != nil {
		t.Fatalf("count imported tokens failed: %v", err)
	}
	if tokenCount != 2 {
		t.Fatalf("expected 2 tokens for managed account, got %d", tokenCount)
	}

	var vipGroup model.SiteUserGroup
	if err := dbpkg.GetDB().Where("site_account_id = ? AND group_key = ?", managed.ID, "vip").First(&vipGroup).Error; err != nil {
		t.Fatalf("expected vip group to be imported: %v", err)
	}
	if vipGroup.Name != "vip" {
		t.Fatalf("expected vip group name, got %q", vipGroup.Name)
	}

	var disabled model.SiteModel
	if err := dbpkg.GetDB().Where("site_account_id = ? AND model_name = ?", managed.ID, "gpt-hidden").First(&disabled).Error; err != nil {
		t.Fatalf("expected disabled site model to be imported: %v", err)
	}
	if !disabled.Disabled {
		t.Fatalf("expected disabled model flag to be true")
	}

	var direct model.SiteAccount
	if err := dbpkg.GetDB().Where("name = ?", "direct-user").First(&direct).Error; err != nil {
		t.Fatalf("query direct account failed: %v", err)
	}
	if direct.CredentialType != model.SiteCredentialTypeAPIKey {
		t.Fatalf("expected direct credential type api_key, got %q", direct.CredentialType)
	}
	if direct.APIKey != "sk-direct-token" {
		t.Fatalf("expected direct account API key, got %q", direct.APIKey)
	}
	if direct.AutoCheckin {
		t.Fatalf("expected direct account auto checkin disabled")
	}

	result, err = SiteImportMetAPI(ctx, mustJSONMarshal(t, buildMetAPIImportPayload("metapi-user-renamed")))
	if err != nil {
		t.Fatalf("second SiteImportMetAPI failed: %v", err)
	}
	if result.CreatedSites != 0 {
		t.Fatalf("expected 0 created sites on second import, got %d", result.CreatedSites)
	}
	if result.ReusedSites != 2 {
		t.Fatalf("expected 2 reused sites on second import, got %d", result.ReusedSites)
	}
	if result.CreatedAccounts != 0 {
		t.Fatalf("expected 0 created accounts on second import, got %d", result.CreatedAccounts)
	}
	if result.UpdatedAccounts != 2 {
		t.Fatalf("expected 2 updated accounts on second import, got %d", result.UpdatedAccounts)
	}

	var accountCount int64
	if err := dbpkg.GetDB().Model(&model.SiteAccount{}).Count(&accountCount).Error; err != nil {
		t.Fatalf("count accounts after second import failed: %v", err)
	}
	if accountCount != 2 {
		t.Fatalf("expected 2 accounts after second import, got %d", accountCount)
	}
}

func TestSiteImportMetAPIInvalidJSONUsesStableMessage(t *testing.T) {
	ctx := setupSiteOpTestDB(t)

	_, err := SiteImportMetAPI(ctx, []byte("{bad json"))
	if err == nil {
		t.Fatalf("expected invalid JSON error")
	}
	if !strings.Contains(err.Error(), "site import invalid json") {
		t.Fatalf("expected stable invalid JSON message, got %q", err.Error())
	}
	if got := apperror.Code(err); got != CodeSiteImportInvalidJSON {
		t.Fatalf("expected error code %q, got %q", CodeSiteImportInvalidJSON, got)
	}
}

func TestSiteModelRouteUpdateIfNotManualHonorsManualOverride(t *testing.T) {
	ctx := setupSiteOpTestDB(t)

	site := &model.Site{
		Name:     "route-guard-site",
		Platform: model.SitePlatformNewAPI,
		BaseURL:  "https://example.com",
		Enabled:  true,
	}
	if err := SiteCreate(site, ctx); err != nil {
		t.Fatalf("SiteCreate failed: %v", err)
	}

	account := &model.SiteAccount{
		SiteID:         site.ID,
		Name:           "route-guard-account",
		CredentialType: model.SiteCredentialTypeAccessToken,
		AccessToken:    "token",
		Enabled:        true,
	}
	if err := SiteAccountCreate(account, ctx); err != nil {
		t.Fatalf("SiteAccountCreate failed: %v", err)
	}

	rows := []model.SiteModel{
		{
			SiteAccountID:  account.ID,
			GroupKey:       model.SiteDefaultGroupKey,
			ModelName:      "claude-3-haiku",
			RouteType:      model.SiteModelRouteTypeAnthropic,
			RouteSource:    model.SiteModelRouteSourceManualOverride,
			ManualOverride: true,
		},
		{
			SiteAccountID:  account.ID,
			GroupKey:       model.SiteDefaultGroupKey,
			ModelName:      "gpt-4.1",
			RouteType:      model.SiteModelRouteTypeOpenAIChat,
			RouteSource:    model.SiteModelRouteSourceSyncInferred,
			ManualOverride: false,
		},
	}
	if err := dbpkg.GetDB().WithContext(ctx).Create(&rows).Error; err != nil {
		t.Fatalf("create site models failed: %v", err)
	}

	updated, err := SiteModelRouteUpdateIfNotManual(account.ID, model.SiteDefaultGroupKey, "claude-3-haiku", model.SiteModelRouteTypeOpenAIResponse, model.SiteModelRouteSourceRuntimeLearned, "mismatch", ctx)
	if err != nil {
		t.Fatalf("SiteModelRouteUpdateIfNotManual returned error: %v", err)
	}
	if updated {
		t.Fatalf("expected manual override row not to be updated")
	}

	updated, err = SiteModelRouteUpdateIfNotManual(account.ID, model.SiteDefaultGroupKey, "gpt-4.1", model.SiteModelRouteTypeOpenAIResponse, model.SiteModelRouteSourceRuntimeLearned, "mismatch", ctx)
	if err != nil {
		t.Fatalf("SiteModelRouteUpdateIfNotManual returned error: %v", err)
	}
	if !updated {
		t.Fatalf("expected non-manual row to be updated")
	}

	var manualRow model.SiteModel
	if err := dbpkg.GetDB().WithContext(ctx).Where("site_account_id = ? AND model_name = ?", account.ID, "claude-3-haiku").First(&manualRow).Error; err != nil {
		t.Fatalf("query manual row failed: %v", err)
	}
	if manualRow.RouteType != model.SiteModelRouteTypeAnthropic {
		t.Fatalf("expected manual route type to remain anthropic, got %q", manualRow.RouteType)
	}
	if !manualRow.ManualOverride {
		t.Fatalf("expected manual override flag to remain true")
	}

	var learnedRow model.SiteModel
	if err := dbpkg.GetDB().WithContext(ctx).Where("site_account_id = ? AND model_name = ?", account.ID, "gpt-4.1").First(&learnedRow).Error; err != nil {
		t.Fatalf("query learned row failed: %v", err)
	}
	if learnedRow.RouteType != model.SiteModelRouteTypeOpenAIResponse {
		t.Fatalf("expected learned route type openai_response, got %q", learnedRow.RouteType)
	}
	if learnedRow.RouteSource != model.SiteModelRouteSourceRuntimeLearned {
		t.Fatalf("expected learned route source runtime_learned, got %q", learnedRow.RouteSource)
	}
	if learnedRow.ManualOverride {
		t.Fatalf("expected learned row manual override to remain false")
	}
	if learnedRow.RouteRawPayload != "mismatch" {
		t.Fatalf("expected learned payload to be recorded, got %q", learnedRow.RouteRawPayload)
	}
}

func assertImportedAccount(t *testing.T, name string, assertFn func(account model.SiteAccount)) {
	t.Helper()

	var account model.SiteAccount
	if err := dbpkg.GetDB().Where("name = ?", name).First(&account).Error; err != nil {
		t.Fatalf("query site account %q failed: %v", name, err)
	}
	assertFn(account)
}

func buildAllAPIHubImportPayload(managedUsername string) map[string]any {
	return map[string]any{
		"version": "2.0",
		"accounts": map[string]any{
			"accounts": []any{
				map[string]any{
					"id":        "managed-account",
					"site_url":  "https://newapi.example.com",
					"site_type": "new-api",
					"site_name": "Managed Site",
					"authType":  "access_token",
					"account_info": map[string]any{
						"id":           7788,
						"username":     managedUsername,
						"access_token": "managed-session-token",
					},
					"checkIn": map[string]any{
						"autoCheckInEnabled": true,
					},
				},
				map[string]any{
					"id":        "cookie-account",
					"site_url":  "https://onehub.example.com",
					"site_type": "one-hub",
					"site_name": "Cookie Site",
					"username":  "cookie-user",
					"authType":  "cookie",
					"cookieAuth": map[string]any{
						"sessionCookie": "sid=cookie-session",
					},
					"checkIn": map[string]any{
						"autoCheckInEnabled": false,
					},
				},
				map[string]any{
					"id":        "direct-openai-account",
					"site_url":  "https://api.openai.com",
					"site_type": "openai",
					"site_name": "OpenAI Direct",
					"username":  "openai-account",
					"authType":  "access_token",
					"account_info": map[string]any{
						"username":     "openai-account",
						"access_token": "sk-openai-account",
					},
				},
				map[string]any{
					"id":        "sub2api-account",
					"site_url":  "https://sub2api.example.com",
					"site_type": "sub2api",
					"site_name": "Sub2API",
					"authType":  "access_token",
					"account_info": map[string]any{
						"id":           99,
						"username":     "sub2-user",
						"access_token": "sub2-session-token",
					},
					"checkIn": map[string]any{
						"autoCheckInEnabled": true,
					},
				},
				map[string]any{
					"id":        "skipped-none-account",
					"site_url":  "https://skip-none.example.com",
					"site_type": "new-api",
					"site_name": "Skip None",
					"authType":  "none",
					"username":  "skip-none-user",
				},
				map[string]any{
					"id":        "skipped-empty-account",
					"site_url":  "https://skip-empty.example.com",
					"site_type": "new-api",
					"site_name": "Skip Empty",
					"authType":  "access_token",
					"account_info": map[string]any{
						"username": "skip-empty-user",
					},
				},
			},
		},
		"apiCredentialProfiles": map[string]any{
			"version": 2,
			"profiles": []any{
				map[string]any{
					"id":      "profile-openai",
					"name":    "OpenAI Profile",
					"apiType": "openai",
					"baseUrl": "https://api.openai.com/v1",
					"apiKey":  "sk-profile-openai",
				},
				map[string]any{
					"id":      "profile-anthropic",
					"name":    "Claude Profile",
					"apiType": "anthropic",
					"baseUrl": "https://api.anthropic.com/v1",
					"apiKey":  "sk-profile-claude",
				},
				map[string]any{
					"id":      "profile-gemini",
					"name":    "Gemini Profile",
					"apiType": "google",
					"baseUrl": "https://generativelanguage.googleapis.com/v1beta",
					"apiKey":  "gemini-profile-key",
				},
				map[string]any{
					"id":      "profile-compat-fallback",
					"name":    "Compat Profile",
					"apiType": "openai-compatible",
					"baseUrl": "https://compat.example.com/v1",
					"apiKey":  "sk-compat-profile",
				},
			},
		},
	}
}

func buildMetAPIImportPayload(managedUsername string) map[string]any {
	return map[string]any{
		"version":   "2.1",
		"timestamp": 1760000000000,
		"type":      "accounts",
		"accounts": map[string]any{
			"sites": []any{
				map[string]any{
					"id":       1,
					"name":     "metapi-managed",
					"url":      "https://metapi-newapi.example.com",
					"platform": "new-api",
					"status":   "active",
				},
				map[string]any{
					"id":       2,
					"name":     "metapi-openai",
					"url":      "https://api.openai.com/v1",
					"platform": "openai",
					"status":   "active",
				},
			},
			"accounts": []any{
				map[string]any{
					"id":             10,
					"siteId":         1,
					"username":       managedUsername,
					"accessToken":    "metapi-session-token",
					"apiToken":       "",
					"status":         "active",
					"checkinEnabled": true,
					"balance":        12.5,
					"balanceUsed":    3.5,
					"extraConfig":    `{"platformUserId":456,"proxyUrl":"http://127.0.0.1:7890"}`,
				},
				map[string]any{
					"id":             20,
					"siteId":         2,
					"username":       "direct-user",
					"accessToken":    "",
					"apiToken":       "sk-direct-token",
					"status":         "active",
					"checkinEnabled": true,
					"extraConfig":    `{"credentialMode":"apikey"}`,
				},
			},
			"accountTokens": []any{
				map[string]any{
					"id":         100,
					"accountId":  10,
					"name":       "default",
					"token":      "sk-metapi-default",
					"tokenGroup": "default",
					"enabled":    true,
					"isDefault":  true,
					"source":     "manual",
				},
				map[string]any{
					"id":         101,
					"accountId":  10,
					"name":       "vip",
					"token":      "sk-metapi-vip",
					"tokenGroup": "vip",
					"enabled":    true,
					"isDefault":  false,
					"source":     "sync",
				},
				map[string]any{
					"id":         200,
					"accountId":  20,
					"name":       "default",
					"token":      "sk-direct-token",
					"tokenGroup": "default",
					"enabled":    true,
					"isDefault":  true,
					"source":     "manual",
				},
			},
			"manualModels": []any{
				map[string]any{"accountId": 10, "modelName": "gpt-4o"},
				map[string]any{"accountId": 10, "modelName": "claude-3-5-sonnet"},
			},
			"siteDisabledModels": []any{
				map[string]any{"siteId": 1, "modelName": "gpt-hidden"},
			},
			"tokenRoutes": []any{
				map[string]any{"id": 1, "modelPattern": "gpt-*"},
			},
			"routeChannels": []any{
				map[string]any{"id": 1, "routeId": 1, "accountId": 10},
			},
			"routeGroupSources": []any{},
			"downstreamApiKeys": []any{
				map[string]any{"name": "client", "key": "sk-client"},
			},
		},
	}
}

func mustJSONMarshal(t *testing.T, value any) []byte {
	t.Helper()

	payload, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("json.Marshal failed: %v", err)
	}
	return payload
}

func containsAll(messages []string, fragments ...string) bool {
	for _, fragment := range fragments {
		matched := false
		for _, message := range messages {
			if strings.Contains(message, fragment) {
				matched = true
				break
			}
		}
		if !matched {
			return false
		}
	}
	return true
}
