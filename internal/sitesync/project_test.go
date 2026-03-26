package sitesync

import (
	"context"
	"path/filepath"
	"testing"

	dbpkg "github.com/bestruirui/octopus/internal/db"
	"github.com/bestruirui/octopus/internal/model"
	"github.com/bestruirui/octopus/internal/op"
	"github.com/bestruirui/octopus/internal/transformer/outbound"
)

func TestBuildProjectedChannelBaseURL(t *testing.T) {
	tests := []struct {
		name     string
		site     *model.Site
		expected string
	}{
		{
			name:     "new api appends v1",
			site:     &model.Site{Platform: model.SitePlatformNewAPI, BaseURL: "https://example.com"},
			expected: "https://example.com/v1",
		},
		{
			name:     "one hub preserves existing v1",
			site:     &model.Site{Platform: model.SitePlatformOneHub, BaseURL: "https://example.com/v1"},
			expected: "https://example.com/v1",
		},
		{
			name:     "openai preserves custom path and appends v1",
			site:     &model.Site{Platform: model.SitePlatformOpenAI, BaseURL: "https://example.com/openai"},
			expected: "https://example.com/openai/v1",
		},
		{
			name:     "claude appends v1",
			site:     &model.Site{Platform: model.SitePlatformClaude, BaseURL: "https://api.anthropic.com"},
			expected: "https://api.anthropic.com/v1",
		},
		{
			name:     "gemini appends v1",
			site:     &model.Site{Platform: model.SitePlatformGemini, BaseURL: "https://gemini.example.com"},
			expected: "https://gemini.example.com/v1",
		},
		{
			name:     "nil site returns empty",
			site:     nil,
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if actual := buildProjectedChannelBaseURL(tt.site); actual != tt.expected {
				t.Fatalf("expected %q, got %q", tt.expected, actual)
			}
		})
	}
}

func TestProjectAccountSplitsManagedChannelsByOutboundType(t *testing.T) {
	ctx := setupProjectTestDB(t)
	site, account := createProjectionFixture(t, ctx, "")

	channelIDs, err := ProjectAccount(ctx, account.ID)
	if err != nil {
		t.Fatalf("ProjectAccount returned error: %v", err)
	}
	if len(channelIDs) != 3 {
		t.Fatalf("expected 3 managed channels for mixed models, got %d", len(channelIDs))
	}

	channelsByGroup := loadProjectedChannelsByGroupKey(t, ctx, account.ID)
	if len(channelsByGroup) != 3 {
		t.Fatalf("expected 3 bindings, got %d", len(channelsByGroup))
	}

	assertProjectedChannel(t, channelsByGroup, "default", outbound.OutboundTypeOpenAIChat, "gpt-4o-mini", false)
	assertProjectedChannel(t, channelsByGroup, "default::anthropic", outbound.OutboundTypeAnthropic, "claude-3-5-sonnet", true)
	assertProjectedChannel(t, channelsByGroup, "default::gemini", outbound.OutboundTypeGemini, "gemini-2.0-flash", true)

	secondRunIDs, err := ProjectAccount(ctx, account.ID)
	if err != nil {
		t.Fatalf("second ProjectAccount returned error: %v", err)
	}
	if len(secondRunIDs) != 3 {
		t.Fatalf("expected 3 managed channels on second projection, got %d", len(secondRunIDs))
	}

	channelsAfterSecondRun := loadProjectedChannelsByGroupKey(t, ctx, account.ID)
	for groupKey, channel := range channelsByGroup {
		reloaded, ok := channelsAfterSecondRun[groupKey]
		if !ok {
			t.Fatalf("expected binding %q to remain after second projection", groupKey)
		}
		if reloaded.ID != channel.ID {
			t.Fatalf("expected binding %q to reuse channel %d, got %d", groupKey, channel.ID, reloaded.ID)
		}
	}

	if site.OutboundFormatMode != "" {
		t.Fatalf("expected fixture to use platform default outbound mode, got %q", site.OutboundFormatMode)
	}
}

func TestProjectAccountHonorsOpenAIOnlyMode(t *testing.T) {
	ctx := setupProjectTestDB(t)
	_, account := createProjectionFixture(t, ctx, model.OutboundFormatModeOpenAI)

	channelIDs, err := ProjectAccount(ctx, account.ID)
	if err != nil {
		t.Fatalf("ProjectAccount returned error: %v", err)
	}
	if len(channelIDs) != 1 {
		t.Fatalf("expected 1 managed channel in openai_only mode, got %d", len(channelIDs))
	}

	channelsByGroup := loadProjectedChannelsByGroupKey(t, ctx, account.ID)
	if len(channelsByGroup) != 1 {
		t.Fatalf("expected 1 binding in openai_only mode, got %d", len(channelsByGroup))
	}

	channel, ok := channelsByGroup["default"]
	if !ok {
		t.Fatalf("expected default binding in openai_only mode, got %#v", channelsByGroup)
	}
	if channel.Type != outbound.OutboundTypeOpenAIChat {
		t.Fatalf("expected default channel type %q, got %q", outbound.OutboundTypeOpenAIChat, channel.Type)
	}
	if channel.Model != "claude-3-5-sonnet,gemini-2.0-flash,gpt-4o-mini" {
		t.Fatalf("expected mixed models to stay in one OpenAI channel, got %q", channel.Model)
	}
	if len(channel.Keys) != 2 {
		t.Fatalf("expected projected OpenAI-only channel to keep both keys, got %d", len(channel.Keys))
	}
}

func setupProjectTestDB(t *testing.T) context.Context {
	t.Helper()

	if dbpkg.GetDB() != nil {
		_ = dbpkg.Close()
	}

	dbPath := filepath.Join(t.TempDir(), "octopus-project-test.db")
	if err := dbpkg.InitDB("sqlite", dbPath, false); err != nil {
		t.Fatalf("InitDB failed: %v", err)
	}
	t.Cleanup(func() {
		_ = dbpkg.Close()
	})

	return context.Background()
}

func createProjectionFixture(t *testing.T, ctx context.Context, outboundFormatMode model.OutboundFormatMode) (*model.Site, *model.SiteAccount) {
	t.Helper()

	site := &model.Site{
		Name:               "Projection Site",
		Platform:           model.SitePlatformNewAPI,
		BaseURL:            "https://example.com",
		Enabled:            true,
		OutboundFormatMode: outboundFormatMode,
	}
	if err := op.SiteCreate(site, ctx); err != nil {
		t.Fatalf("SiteCreate failed: %v", err)
	}

	account := &model.SiteAccount{
		SiteID:         site.ID,
		Name:           "Primary Account",
		CredentialType: model.SiteCredentialTypeAccessToken,
		AccessToken:    "site-access-token",
		Enabled:        true,
		AutoSync:       false,
		AutoCheckin:    false,
	}
	if err := op.SiteAccountCreate(account, ctx); err != nil {
		t.Fatalf("SiteAccountCreate failed: %v", err)
	}

	tokens := []model.SiteToken{
		{SiteAccountID: account.ID, Name: "primary", Token: "key-primary", GroupKey: "default", GroupName: "default", Enabled: true},
		{SiteAccountID: account.ID, Name: "backup", Token: "key-backup", GroupKey: "default", GroupName: "default", Enabled: true},
	}
	if err := dbpkg.GetDB().WithContext(ctx).Create(&tokens).Error; err != nil {
		t.Fatalf("create site tokens failed: %v", err)
	}

	models := []model.SiteModel{
		{SiteAccountID: account.ID, ModelName: "gpt-4o-mini", Source: "sync"},
		{SiteAccountID: account.ID, ModelName: "claude-3-5-sonnet", Source: "sync"},
		{SiteAccountID: account.ID, ModelName: "gemini-2.0-flash", Source: "sync"},
	}
	if err := dbpkg.GetDB().WithContext(ctx).Create(&models).Error; err != nil {
		t.Fatalf("create site models failed: %v", err)
	}

	return site, account
}

func loadProjectedChannelsByGroupKey(t *testing.T, ctx context.Context, accountID int) map[string]model.Channel {
	t.Helper()

	var bindings []model.SiteChannelBinding
	if err := dbpkg.GetDB().WithContext(ctx).
		Where("site_account_id = ?", accountID).
		Order("group_key ASC").
		Find(&bindings).Error; err != nil {
		t.Fatalf("load site channel bindings failed: %v", err)
	}

	channelsByGroup := make(map[string]model.Channel, len(bindings))
	for _, binding := range bindings {
		var channel model.Channel
		if err := dbpkg.GetDB().WithContext(ctx).
			Preload("Keys").
			First(&channel, binding.ChannelID).Error; err != nil {
			t.Fatalf("load channel %d failed: %v", binding.ChannelID, err)
		}
		channelsByGroup[binding.GroupKey] = channel
	}

	return channelsByGroup
}

func assertProjectedChannel(t *testing.T, channelsByGroup map[string]model.Channel, groupKey string, expectedType outbound.OutboundType, expectedModel string, wantSuffix bool) {
	t.Helper()

	channel, ok := channelsByGroup[groupKey]
	if !ok {
		t.Fatalf("expected projected channel for group key %q, got %#v", groupKey, channelsByGroup)
	}
	if channel.Type != expectedType {
		t.Fatalf("expected channel %q type %q, got %q", groupKey, expectedType, channel.Type)
	}
	if channel.Model != expectedModel {
		t.Fatalf("expected channel %q model %q, got %q", groupKey, expectedModel, channel.Model)
	}
	if len(channel.BaseUrls) != 1 || channel.BaseUrls[0].URL != "https://example.com/v1" {
		t.Fatalf("expected channel %q base URL to be projected with /v1 suffix, got %#v", groupKey, channel.BaseUrls)
	}
	if len(channel.Keys) != 2 {
		t.Fatalf("expected channel %q to carry both projected keys, got %d", groupKey, len(channel.Keys))
	}
	if wantSuffix {
		if groupKey == "default::anthropic" && channel.Name != "[Site] Projection Site / Primary Account / default (default) [Anthropic]" {
			t.Fatalf("unexpected anthropic channel name: %q", channel.Name)
		}
		if groupKey == "default::gemini" && channel.Name != "[Site] Projection Site / Primary Account / default (default) [Gemini]" {
			t.Fatalf("unexpected gemini channel name: %q", channel.Name)
		}
		return
	}
	if channel.Name != "[Site] Projection Site / Primary Account / default (default)" {
		t.Fatalf("unexpected OpenAI/default channel name: %q", channel.Name)
	}
}
