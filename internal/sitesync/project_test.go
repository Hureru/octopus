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
	_, account := createProjectionFixture(t, ctx)

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
}

func TestProjectAccountSupportsAllConfiguredRouteBuckets(t *testing.T) {
	ctx := setupProjectTestDB(t)
	_, account := createProjectionFixture(t, ctx)

	extraModels := []model.SiteModel{
		{SiteAccountID: account.ID, GroupKey: model.SiteDefaultGroupKey, ModelName: "text-embedding-3-large", Source: "sync", RouteType: model.SiteModelRouteTypeOpenAIEmbedding},
		{SiteAccountID: account.ID, GroupKey: model.SiteDefaultGroupKey, ModelName: "doubao-seed-1-6", Source: "sync", RouteType: model.SiteModelRouteTypeVolcengine},
	}
	if err := dbpkg.GetDB().WithContext(ctx).Create(&extraModels).Error; err != nil {
		t.Fatalf("create extra site models failed: %v", err)
	}

	channelIDs, err := ProjectAccount(ctx, account.ID)
	if err != nil {
		t.Fatalf("ProjectAccount returned error: %v", err)
	}
	if len(channelIDs) != 5 {
		t.Fatalf("expected 5 managed channels for 5 route buckets, got %d", len(channelIDs))
	}

	channelsByGroup := loadProjectedChannelsByGroupKey(t, ctx, account.ID)
	if len(channelsByGroup) != 5 {
		t.Fatalf("expected 5 bindings, got %d", len(channelsByGroup))
	}

	assertProjectedChannel(t, channelsByGroup, "default", outbound.OutboundTypeOpenAIChat, "gpt-4o-mini", false)
	assertProjectedChannel(t, channelsByGroup, "default::anthropic", outbound.OutboundTypeAnthropic, "claude-3-5-sonnet", true)
	assertProjectedChannel(t, channelsByGroup, "default::gemini", outbound.OutboundTypeGemini, "gemini-2.0-flash", true)
	assertProjectedChannel(t, channelsByGroup, "default::volcengine", outbound.OutboundTypeVolcengine, "doubao-seed-1-6", true)
	assertProjectedChannel(t, channelsByGroup, "default::openai-embedding", outbound.OutboundTypeOpenAIEmbedding, "text-embedding-3-large", true)
}

func TestProjectAccountRewritesGroupItemsBeforeRemovingStaleManagedBindings(t *testing.T) {
	ctx := setupProjectTestDB(t)
	_, account := createProjectionFixture(t, ctx)

	if _, err := ProjectAccount(ctx, account.ID); err != nil {
		t.Fatalf("initial ProjectAccount returned error: %v", err)
	}

	channelsByGroup := loadProjectedChannelsByGroupKey(t, ctx, account.ID)
	openAIChannel, ok := channelsByGroup["default"]
	if !ok {
		t.Fatalf("expected default projected channel to exist")
	}
	anthropicChannel, ok := channelsByGroup["default::anthropic"]
	if !ok {
		t.Fatalf("expected anthropic projected channel to exist")
	}

	group := &model.Group{Name: "rewrite-managed-items", Mode: model.GroupModeFailover}
	if err := op.GroupCreate(group, ctx); err != nil {
		t.Fatalf("GroupCreate failed: %v", err)
	}
	if err := op.GroupItemAdd(&model.GroupItem{
		GroupID:   group.ID,
		ChannelID: anthropicChannel.ID,
		ModelName: "claude-3-5-sonnet",
		Priority:  1,
		Weight:    1,
	}, ctx); err != nil {
		t.Fatalf("GroupItemAdd failed: %v", err)
	}

	if err := dbpkg.GetDB().WithContext(ctx).
		Model(&model.SiteModel{}).
		Where("site_account_id = ? AND group_key = ? AND model_name = ?", account.ID, model.SiteDefaultGroupKey, "claude-3-5-sonnet").
		Update("route_type", model.SiteModelRouteTypeOpenAIChat).Error; err != nil {
		t.Fatalf("updating site model route_type failed: %v", err)
	}

	if _, err := ProjectAccount(ctx, account.ID); err != nil {
		t.Fatalf("second ProjectAccount returned error: %v", err)
	}

	items, err := op.GroupItemList(group.ID, ctx)
	if err != nil {
		t.Fatalf("GroupItemList failed: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("expected rewritten group item to remain, got %d items", len(items))
	}
	if items[0].ChannelID != openAIChannel.ID {
		t.Fatalf("expected group item to be rewritten onto OpenAI channel %d, got %d", openAIChannel.ID, items[0].ChannelID)
	}

	bindings := loadProjectedChannelsByGroupKey(t, ctx, account.ID)
	if _, ok := bindings["default::anthropic"]; ok {
		t.Fatalf("expected stale anthropic binding to be removed after route rewrite")
	}
}

func TestProjectAccountRemovesUnsupportedModelsFromProjectedChannels(t *testing.T) {
	ctx := setupProjectTestDB(t)
	_, account := createProjectionFixture(t, ctx)

	extraModel := model.SiteModel{
		SiteAccountID: account.ID,
		GroupKey:      model.SiteDefaultGroupKey,
		ModelName:     "vendor-embedding-x",
		Source:        "sync",
		RouteType:     model.SiteModelRouteTypeOpenAIChat,
		RouteSource:   model.SiteModelRouteSourceSyncInferred,
	}
	if err := dbpkg.GetDB().WithContext(ctx).Create(&extraModel).Error; err != nil {
		t.Fatalf("create extra site model failed: %v", err)
	}

	if _, err := ProjectAccount(ctx, account.ID); err != nil {
		t.Fatalf("initial ProjectAccount returned error: %v", err)
	}

	channelsByGroup := loadProjectedChannelsByGroupKey(t, ctx, account.ID)
	openAIChannel, ok := channelsByGroup["default"]
	if !ok {
		t.Fatalf("expected default projected channel to exist")
	}
	if openAIChannel.Model != "gpt-4o-mini,vendor-embedding-x" {
		t.Fatalf("expected default channel to include vendor model before it becomes unsupported, got %q", openAIChannel.Model)
	}

	group := &model.Group{Name: "remove-unsupported-managed-items", Mode: model.GroupModeFailover}
	if err := op.GroupCreate(group, ctx); err != nil {
		t.Fatalf("GroupCreate failed: %v", err)
	}
	if err := op.GroupItemAdd(&model.GroupItem{
		GroupID:   group.ID,
		ChannelID: openAIChannel.ID,
		ModelName: "vendor-embedding-x",
		Priority:  1,
		Weight:    1,
	}, ctx); err != nil {
		t.Fatalf("GroupItemAdd failed: %v", err)
	}

	unsupportedPayload := model.SiteModelRouteMetadata{
		Source:                 "/api/pricing",
		RouteSupported:         false,
		SupportedEndpointTypes: []string{"/vendor/embeddings"},
		UnsupportedReason:      "site reports endpoint types outside current supported route buckets",
	}.Marshal()
	if err := dbpkg.GetDB().WithContext(ctx).
		Model(&model.SiteModel{}).
		Where("site_account_id = ? AND group_key = ? AND model_name = ?", account.ID, model.SiteDefaultGroupKey, "vendor-embedding-x").
		Updates(map[string]any{
			"route_type":        model.SiteModelRouteTypeUnknown,
			"route_raw_payload": unsupportedPayload,
			"route_source":      model.SiteModelRouteSourceSyncInferred,
		}).Error; err != nil {
		t.Fatalf("updating vendor model route_type failed: %v", err)
	}

	if _, err := ProjectAccount(ctx, account.ID); err != nil {
		t.Fatalf("second ProjectAccount returned error: %v", err)
	}

	reloadedChannels := loadProjectedChannelsByGroupKey(t, ctx, account.ID)
	reloadedOpenAIChannel, ok := reloadedChannels["default"]
	if !ok {
		t.Fatalf("expected default projected channel to remain after unsupported model removal")
	}
	if reloadedOpenAIChannel.Model != "gpt-4o-mini" {
		t.Fatalf("expected unsupported model to be removed from default channel, got %q", reloadedOpenAIChannel.Model)
	}

	items, err := op.GroupItemList(group.ID, ctx)
	if err != nil {
		t.Fatalf("GroupItemList failed: %v", err)
	}
	if len(items) != 0 {
		t.Fatalf("expected group items for unsupported model to be removed, got %d items", len(items))
	}
}

func TestProjectAccountReusesOrphanManagedChannelWithSameName(t *testing.T) {
	ctx := setupProjectTestDB(t)

	site := &model.Site{
		Name:     "DoneHub Projection Site",
		Platform: model.SitePlatformDoneHub,
		BaseURL:  "https://donehub.example.com",
		Enabled:  true,
	}
	if err := op.SiteCreate(site, ctx); err != nil {
		t.Fatalf("SiteCreate failed: %v", err)
	}

	account := &model.SiteAccount{
		SiteID:         site.ID,
		Name:           "Primary Account",
		CredentialType: model.SiteCredentialTypeAccessToken,
		AccessToken:    "donehub-session-token",
		Enabled:        true,
	}
	if err := op.SiteAccountCreate(account, ctx); err != nil {
		t.Fatalf("SiteAccountCreate failed: %v", err)
	}

	token := model.SiteToken{
		SiteAccountID: account.ID,
		Name:          "primary",
		Token:         "key-primary",
		GroupKey:      model.SiteDefaultGroupKey,
		GroupName:     model.SiteDefaultGroupName,
		Enabled:       true,
	}
	if err := dbpkg.GetDB().WithContext(ctx).Create(&token).Error; err != nil {
		t.Fatalf("create site token failed: %v", err)
	}

	siteModel := model.SiteModel{
		SiteAccountID: account.ID,
		GroupKey:      model.SiteDefaultGroupKey,
		ModelName:     "gpt-4o-mini",
		Source:        "sync",
		RouteType:     model.SiteModelRouteTypeOpenAIChat,
		RouteSource:   model.SiteModelRouteSourceSyncInferred,
	}
	if err := dbpkg.GetDB().WithContext(ctx).Create(&siteModel).Error; err != nil {
		t.Fatalf("create site model failed: %v", err)
	}

	group := model.SiteUserGroup{GroupKey: model.SiteDefaultGroupKey, Name: model.SiteDefaultGroupName}
	orphanName := buildManagedChannelName(site, account, group, outbound.OutboundTypeOpenAIChat, shouldSplitByOutboundType(site))
	orphanChannel := model.Channel{
		Name:      orphanName,
		Type:      outbound.OutboundTypeOpenAIChat,
		Enabled:   true,
		BaseUrls:  []model.BaseUrl{{URL: "https://donehub.example.com/v1", Delay: 0}},
		Model:     "stale-model",
		AutoGroup: model.AutoGroupTypeNone,
	}
	if err := op.ChannelCreate(&orphanChannel, ctx); err != nil {
		t.Fatalf("creating orphan channel failed: %v", err)
	}

	channelIDs, err := ProjectAccount(ctx, account.ID)
	if err != nil {
		t.Fatalf("ProjectAccount returned error: %v", err)
	}
	if len(channelIDs) != 1 {
		t.Fatalf("expected one projected channel, got %d", len(channelIDs))
	}
	if channelIDs[0] != orphanChannel.ID {
		t.Fatalf("expected orphan channel %d to be reused, got %v", orphanChannel.ID, channelIDs)
	}

	var binding model.SiteChannelBinding
	if err := dbpkg.GetDB().WithContext(ctx).Where("site_account_id = ?", account.ID).First(&binding).Error; err != nil {
		t.Fatalf("expected reused channel to gain binding: %v", err)
	}
	if binding.ChannelID != orphanChannel.ID {
		t.Fatalf("expected binding to point to reused orphan channel %d, got %d", orphanChannel.ID, binding.ChannelID)
	}

	reloaded, err := op.ChannelGet(orphanChannel.ID, ctx)
	if err != nil {
		t.Fatalf("ChannelGet failed: %v", err)
	}
	if reloaded.Model != "gpt-4o-mini" {
		t.Fatalf("expected reused orphan channel model to be refreshed, got %q", reloaded.Model)
	}
}

func TestProjectAccountPreservesManagedKeyUsageForUnchangedTokens(t *testing.T) {
	ctx := setupProjectTestDB(t)
	_, account := createProjectionFixture(t, ctx)

	if _, err := ProjectAccount(ctx, account.ID); err != nil {
		t.Fatalf("initial ProjectAccount returned error: %v", err)
	}

	channelsByGroup := loadProjectedChannelsByGroupKey(t, ctx, account.ID)
	channel := channelsByGroup["default"]
	if len(channel.Keys) == 0 {
		t.Fatalf("expected projected channel keys to exist")
	}

	firstKey := channel.Keys[0]
	firstKey.TotalCost = 12.34
	firstKey.StatusCode = 200
	if err := op.ChannelKeyUpdate(firstKey); err != nil {
		t.Fatalf("ChannelKeyUpdate failed: %v", err)
	}
	if err := op.ChannelKeySaveDB(ctx); err != nil {
		t.Fatalf("ChannelKeySaveDB failed: %v", err)
	}

	if _, err := ProjectAccount(ctx, account.ID); err != nil {
		t.Fatalf("second ProjectAccount returned error: %v", err)
	}

	reloadedChannel, err := op.ChannelGet(channel.ID, ctx)
	if err != nil {
		t.Fatalf("ChannelGet failed: %v", err)
	}
	if len(reloadedChannel.Keys) != len(channel.Keys) {
		t.Fatalf("expected %d keys after reprojection, got %d", len(channel.Keys), len(reloadedChannel.Keys))
	}

	var preserved *model.ChannelKey
	for i := range reloadedChannel.Keys {
		if reloadedChannel.Keys[i].ChannelKey == firstKey.ChannelKey {
			preserved = &reloadedChannel.Keys[i]
			break
		}
	}
	if preserved == nil {
		t.Fatalf("expected key %q to remain after reprojection", firstKey.ChannelKey)
	}
	if preserved.ID != firstKey.ID {
		t.Fatalf("expected unchanged token to keep key id %d, got %d", firstKey.ID, preserved.ID)
	}
	if preserved.TotalCost != firstKey.TotalCost {
		t.Fatalf("expected unchanged token to preserve total cost %.2f, got %.2f", firstKey.TotalCost, preserved.TotalCost)
	}
}

func TestProjectAccountSyncsProjectedModelPrices(t *testing.T) {
	ctx := setupProjectTestDB(t)
	_, account := createProjectionFixture(t, ctx)

	if _, err := ProjectAccount(ctx, account.ID); err != nil {
		t.Fatalf("ProjectAccount returned error: %v", err)
	}

	if got, err := op.LLMGet("gpt-4o-mini"); err != nil {
		t.Fatalf("expected gpt-4o-mini price to be inserted: %v", err)
	} else if got.Input <= 0 || got.Output <= 0 {
		t.Fatalf("unexpected projected price for gpt-4o-mini: %+v", got)
	}
}

func TestDeleteSiteAccountRemovesManagedChannelChain(t *testing.T) {
	ctx := setupProjectTestDB(t)
	site, account := createProjectionFixture(t, ctx)

	channelIDs, err := ProjectAccount(ctx, account.ID)
	if err != nil {
		t.Fatalf("ProjectAccount returned error: %v", err)
	}
	if len(channelIDs) == 0 {
		t.Fatalf("expected managed channels to be created")
	}

	group := &model.Group{Name: "managed-delete-group", Mode: model.GroupModeFailover}
	if err := op.GroupCreate(group, ctx); err != nil {
		t.Fatalf("GroupCreate failed: %v", err)
	}
	if err := op.GroupItemAdd(&model.GroupItem{
		GroupID:   group.ID,
		ChannelID: channelIDs[0],
		ModelName: "gpt-4o-mini",
		Priority:  1,
		Weight:    1,
	}, ctx); err != nil {
		t.Fatalf("GroupItemAdd failed: %v", err)
	}
	if err := op.StatsChannelUpdate(channelIDs[0], model.StatsMetrics{InputCost: 1, OutputCost: 2, RequestSuccess: 1}); err != nil {
		t.Fatalf("StatsChannelUpdate failed: %v", err)
	}
	if err := op.StatsSaveDB(ctx); err != nil {
		t.Fatalf("StatsSaveDB failed: %v", err)
	}

	if err := DeleteSiteAccount(ctx, account.ID); err != nil {
		t.Fatalf("DeleteSiteAccount returned error: %v", err)
	}

	if _, err := op.SiteGet(site.ID, ctx); err != nil {
		t.Fatalf("site should remain after account deletion: %v", err)
	}
	if _, err := op.SiteAccountGet(account.ID, ctx); err == nil {
		t.Fatalf("expected site account to be deleted")
	}

	var bindingCount int64
	if err := dbpkg.GetDB().WithContext(ctx).Model(&model.SiteChannelBinding{}).Where("site_account_id = ?", account.ID).Count(&bindingCount).Error; err != nil {
		t.Fatalf("count bindings failed: %v", err)
	}
	if bindingCount != 0 {
		t.Fatalf("expected bindings to be deleted, got %d", bindingCount)
	}

	var tokenCount int64
	if err := dbpkg.GetDB().WithContext(ctx).Model(&model.SiteToken{}).Where("site_account_id = ?", account.ID).Count(&tokenCount).Error; err != nil {
		t.Fatalf("count tokens failed: %v", err)
	}
	if tokenCount != 0 {
		t.Fatalf("expected tokens to be deleted, got %d", tokenCount)
	}

	var modelCount int64
	if err := dbpkg.GetDB().WithContext(ctx).Model(&model.SiteModel{}).Where("site_account_id = ?", account.ID).Count(&modelCount).Error; err != nil {
		t.Fatalf("count models failed: %v", err)
	}
	if modelCount != 0 {
		t.Fatalf("expected site models to be deleted, got %d", modelCount)
	}

	for _, channelID := range channelIDs {
		if _, err := op.ChannelGet(channelID, ctx); err == nil {
			t.Fatalf("expected managed channel %d to be deleted", channelID)
		}
		stats := op.StatsChannelGet(channelID)
		if stats.ChannelID != channelID || stats.InputCost != 0 || stats.OutputCost != 0 || stats.RequestSuccess != 0 {
			t.Fatalf("expected in-memory stats for channel %d to be cleared, got %+v", channelID, stats)
		}
		var statsCount int64
		if err := dbpkg.GetDB().WithContext(ctx).Model(&model.StatsChannel{}).Where("channel_id = ?", channelID).Count(&statsCount).Error; err != nil {
			t.Fatalf("count stats failed: %v", err)
		}
		if statsCount != 0 {
			t.Fatalf("expected persisted stats for channel %d to be deleted, got %d", channelID, statsCount)
		}
	}

	items, err := op.GroupItemList(group.ID, ctx)
	if err != nil {
		t.Fatalf("GroupItemList failed: %v", err)
	}
	if len(items) != 0 {
		t.Fatalf("expected group items referencing managed channels to be deleted, got %d", len(items))
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

func createProjectionFixture(t *testing.T, ctx context.Context) (*model.Site, *model.SiteAccount) {
	t.Helper()

	site := &model.Site{
		Name:     "Projection Site",
		Platform: model.SitePlatformNewAPI,
		BaseURL:  "https://example.com",
		Enabled:  true,
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
		{SiteAccountID: account.ID, GroupKey: model.SiteDefaultGroupKey, ModelName: "gpt-4o-mini", Source: "sync", RouteType: model.SiteModelRouteTypeOpenAIChat, RouteSource: model.SiteModelRouteSourceSyncInferred},
		{SiteAccountID: account.ID, GroupKey: model.SiteDefaultGroupKey, ModelName: "claude-3-5-sonnet", Source: "sync", RouteType: model.SiteModelRouteTypeAnthropic, RouteSource: model.SiteModelRouteSourceSyncInferred},
		{SiteAccountID: account.ID, GroupKey: model.SiteDefaultGroupKey, ModelName: "gemini-2.0-flash", Source: "sync", RouteType: model.SiteModelRouteTypeGemini, RouteSource: model.SiteModelRouteSourceSyncInferred},
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
		if groupKey == "default::volcengine" && channel.Name != "[Site] Projection Site / Primary Account / default (default) [Volcengine]" {
			t.Fatalf("unexpected volcengine channel name: %q", channel.Name)
		}
		if groupKey == "default::openai-embedding" && channel.Name != "[Site] Projection Site / Primary Account / default (default) [OpenAI Embedding]" {
			t.Fatalf("unexpected embedding channel name: %q", channel.Name)
		}
		return
	}
	if channel.Name != "[Site] Projection Site / Primary Account / default (default)" {
		t.Fatalf("unexpected OpenAI/default channel name: %q", channel.Name)
	}
}
