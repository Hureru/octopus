package op

import (
	"context"
	"path/filepath"
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
