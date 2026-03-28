package op

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

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

func TestSiteUpdateRejectsInvalidOutboundFormatMode(t *testing.T) {
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

	invalidMode := model.OutboundFormatMode("invalid-mode")
	if _, err := SiteUpdate(&model.SiteUpdateRequest{
		ID:                 site.ID,
		OutboundFormatMode: &invalidMode,
	}, ctx); err == nil {
		t.Fatalf("expected SiteUpdate to reject invalid outbound format mode")
	}

	reloaded, err := SiteGet(site.ID, ctx)
	if err != nil {
		t.Fatalf("SiteGet failed: %v", err)
	}
	if reloaded.OutboundFormatMode != "" {
		t.Fatalf("expected outbound format mode to remain empty, got %q", reloaded.OutboundFormatMode)
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
