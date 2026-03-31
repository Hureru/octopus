package op

import (
	"testing"

	dbpkg "github.com/bestruirui/octopus/internal/db"
	"github.com/bestruirui/octopus/internal/model"
	"github.com/bestruirui/octopus/internal/transformer/outbound"
	"gorm.io/gorm"
)

func TestSiteChannelResetAccountRoutesRestoresDetectedMetadataRoute(t *testing.T) {
	ctx := setupSiteOpTestDB(t)

	site := &model.Site{
		Name:     "site-channel-reset-site",
		Platform: model.SitePlatformNewAPI,
		BaseURL:  "https://example.com",
		Enabled:  true,
	}
	if err := SiteCreate(site, ctx); err != nil {
		t.Fatalf("SiteCreate failed: %v", err)
	}

	account := &model.SiteAccount{
		SiteID:         site.ID,
		Name:           "site-channel-reset-account",
		CredentialType: model.SiteCredentialTypeAccessToken,
		AccessToken:    "token",
		Enabled:        true,
	}
	if err := SiteAccountCreate(account, ctx); err != nil {
		t.Fatalf("SiteAccountCreate failed: %v", err)
	}

	routePayload := model.SiteModelRouteMetadata{
		Source:                  "/api/pricing",
		RouteSupported:          true,
		RouteType:               model.SiteModelRouteTypeOpenAIResponse,
		SupportedEndpointTypes:  []string{"/v1/responses"},
		NormalizedEndpointTypes: []string{string(model.SiteModelRouteTypeOpenAIResponse)},
	}.Marshal()
	row := model.SiteModel{
		SiteAccountID:   account.ID,
		GroupKey:        model.SiteDefaultGroupKey,
		ModelName:       "gpt-4o-mini",
		RouteType:       model.SiteModelRouteTypeAnthropic,
		RouteSource:     model.SiteModelRouteSourceManualOverride,
		ManualOverride:  true,
		RouteRawPayload: routePayload,
	}
	if err := dbpkg.GetDB().WithContext(ctx).Create(&row).Error; err != nil {
		t.Fatalf("create site model failed: %v", err)
	}

	if err := SiteChannelResetAccountRoutes(site.ID, account.ID, ctx); err != nil {
		t.Fatalf("SiteChannelResetAccountRoutes failed: %v", err)
	}

	var reloaded model.SiteModel
	if err := dbpkg.GetDB().WithContext(ctx).Where("id = ?", row.ID).First(&reloaded).Error; err != nil {
		t.Fatalf("query reloaded site model failed: %v", err)
	}
	if reloaded.RouteType != model.SiteModelRouteTypeOpenAIResponse {
		t.Fatalf("expected reset route type %q, got %q", model.SiteModelRouteTypeOpenAIResponse, reloaded.RouteType)
	}
	if reloaded.ManualOverride {
		t.Fatalf("expected manual override to be cleared")
	}
	if reloaded.RouteRawPayload != routePayload {
		t.Fatalf("expected reset to keep route metadata payload, got %q", reloaded.RouteRawPayload)
	}
}

func TestSiteChannelAccountGetIncludesParsedRouteMetadata(t *testing.T) {
	ctx := setupSiteOpTestDB(t)

	site := &model.Site{
		Name:     "site-channel-view-site",
		Platform: model.SitePlatformNewAPI,
		BaseURL:  "https://example.com",
		Enabled:  true,
	}
	if err := SiteCreate(site, ctx); err != nil {
		t.Fatalf("SiteCreate failed: %v", err)
	}

	account := &model.SiteAccount{
		SiteID:         site.ID,
		Name:           "site-channel-view-account",
		CredentialType: model.SiteCredentialTypeAccessToken,
		AccessToken:    "token",
		Enabled:        true,
	}
	if err := SiteAccountCreate(account, ctx); err != nil {
		t.Fatalf("SiteAccountCreate failed: %v", err)
	}

	payload := model.SiteModelRouteMetadata{
		Source:                 "/api/pricing",
		RouteSupported:         false,
		SupportedEndpointTypes: []string{"/vendor/embeddings"},
		UnsupportedReason:      "site reports endpoint types outside current supported route buckets",
	}.Marshal()
	row := model.SiteModel{
		SiteAccountID:   account.ID,
		GroupKey:        model.SiteDefaultGroupKey,
		ModelName:       "vendor-embedding-x",
		RouteType:       model.SiteModelRouteTypeUnknown,
		RouteSource:     model.SiteModelRouteSourceSyncInferred,
		RouteRawPayload: payload,
	}
	if err := dbpkg.GetDB().WithContext(ctx).Create(&row).Error; err != nil {
		t.Fatalf("create site model failed: %v", err)
	}

	view, err := SiteChannelAccountGet(site.ID, account.ID, ctx)
	if err != nil {
		t.Fatalf("SiteChannelAccountGet failed: %v", err)
	}
	if len(view.Groups) != 1 || len(view.Groups[0].Models) != 1 {
		t.Fatalf("unexpected site channel view: %+v", view.Groups)
	}
	modelView := view.Groups[0].Models[0]
	if modelView.RouteMetadata == nil {
		t.Fatalf("expected route metadata to be included in site channel model view")
	}
	if modelView.RouteMetadata.RouteSupported {
		t.Fatalf("expected parsed route metadata to remain unsupported")
	}
	if modelView.RouteMetadata.RouteType != model.SiteModelRouteTypeUnknown {
		t.Fatalf("expected unsupported route type %q, got %q", model.SiteModelRouteTypeUnknown, modelView.RouteMetadata.RouteType)
	}
}

func TestSiteChannelAccountGetIncludesFullProjectedKeys(t *testing.T) {
	ctx := setupSiteOpTestDB(t)

	site := &model.Site{
		Name:     "site-channel-projected-key-site",
		Platform: model.SitePlatformNewAPI,
		BaseURL:  "https://example.com",
		Enabled:  true,
	}
	if err := SiteCreate(site, ctx); err != nil {
		t.Fatalf("SiteCreate failed: %v", err)
	}

	account := &model.SiteAccount{
		SiteID:         site.ID,
		Name:           "site-channel-projected-key-account",
		CredentialType: model.SiteCredentialTypeAccessToken,
		AccessToken:    "token",
		Enabled:        true,
	}
	if err := SiteAccountCreate(account, ctx); err != nil {
		t.Fatalf("SiteAccountCreate failed: %v", err)
	}

	group := model.SiteUserGroup{SiteAccountID: account.ID, GroupKey: model.SiteDefaultGroupKey, Name: model.SiteDefaultGroupName}
	if err := dbpkg.GetDB().WithContext(ctx).Create(&group).Error; err != nil {
		t.Fatalf("create site group failed: %v", err)
	}

	channel := &model.Channel{
		Name:    "managed-channel",
		Type:    outbound.OutboundTypeOpenAIChat,
		Enabled: true,
		BaseUrls: []model.BaseUrl{{
			URL:   "https://example.com/v1",
			Delay: 0,
		}},
		Keys: []model.ChannelKey{{
			Enabled:    true,
			ChannelKey: "sk-managed-secret-key",
			Remark:     "default",
		}},
		Model:       "gpt-4o-mini",
		AutoSync:    false,
		AutoGroup:   model.AutoGroupTypeNone,
	}
	if err := ChannelCreate(channel, ctx); err != nil {
		t.Fatalf("ChannelCreate failed: %v", err)
	}

	binding := model.SiteChannelBinding{
		SiteID:          site.ID,
		SiteAccountID:   account.ID,
		SiteUserGroupID: &group.ID,
		GroupKey:        model.SiteDefaultGroupKey,
		ChannelID:       channel.ID,
	}
	if err := dbpkg.GetDB().WithContext(ctx).Create(&binding).Error; err != nil {
		t.Fatalf("create site channel binding failed: %v", err)
	}

	view, err := SiteChannelAccountGet(site.ID, account.ID, ctx)
	if err != nil {
		t.Fatalf("SiteChannelAccountGet failed: %v", err)
	}
	if len(view.Groups) != 1 {
		t.Fatalf("expected one group, got %+v", view.Groups)
	}
	if len(view.Groups[0].ProjectedKeys) != 1 {
		t.Fatalf("expected one projected key, got %+v", view.Groups[0].ProjectedKeys)
	}
	projectedKey := view.Groups[0].ProjectedKeys[0]
	if projectedKey.ChannelKey != "sk-managed-secret-key" {
		t.Fatalf("expected full projected key, got %q", projectedKey.ChannelKey)
	}
	if projectedKey.ChannelKeyMasked != "sk-m...-key" {
		t.Fatalf("expected masked projected key, got %q", projectedKey.ChannelKeyMasked)
	}
}

func TestSiteChannelAccountGetShowsExplicitGroupModelsWithoutKeys(t *testing.T) {
	ctx := setupSiteOpTestDB(t)

	site := &model.Site{
		Name:     "site-channel-explicit-groups-site",
		Platform: model.SitePlatformNewAPI,
		BaseURL:  "https://example.com",
		Enabled:  true,
	}
	if err := SiteCreate(site, ctx); err != nil {
		t.Fatalf("SiteCreate failed: %v", err)
	}

	account := &model.SiteAccount{
		SiteID:         site.ID,
		Name:           "site-channel-explicit-groups-account",
		CredentialType: model.SiteCredentialTypeAccessToken,
		AccessToken:    "token",
		Enabled:        true,
	}
	if err := SiteAccountCreate(account, ctx); err != nil {
		t.Fatalf("SiteAccountCreate failed: %v", err)
	}

	groups := []model.SiteUserGroup{
		{SiteAccountID: account.ID, GroupKey: "default", Name: "default"},
		{SiteAccountID: account.ID, GroupKey: "vip", Name: "VIP"},
	}
	if err := dbpkg.GetDB().WithContext(ctx).Create(&groups).Error; err != nil {
		t.Fatalf("create site groups failed: %v", err)
	}

	if err := dbpkg.GetDB().WithContext(ctx).Create(&model.SiteToken{
		SiteAccountID: account.ID,
		Name:          "default-key",
		Token:         "managed-key",
		GroupKey:      "default",
		GroupName:     "default",
		Enabled:       true,
	}).Error; err != nil {
		t.Fatalf("create site token failed: %v", err)
	}

	payload := model.SiteModelRouteMetadata{
		Source:                 "/api/pricing",
		RouteSupported:         true,
		RouteType:              model.SiteModelRouteTypeOpenAIChat,
		EnableGroups:           []string{"default", "vip"},
		SupportedEndpointTypes: []string{"/v1/chat/completions"},
	}.Marshal()
	rows := []model.SiteModel{
		{
			SiteAccountID:   account.ID,
			GroupKey:        "default",
			ModelName:       "gpt-4o-mini",
			RouteType:       model.SiteModelRouteTypeOpenAIChat,
			RouteSource:     model.SiteModelRouteSourceSyncInferred,
			RouteRawPayload: payload,
		},
		{
			SiteAccountID:   account.ID,
			GroupKey:        "vip",
			ModelName:       "gpt-4o-mini",
			RouteType:       model.SiteModelRouteTypeOpenAIChat,
			RouteSource:     model.SiteModelRouteSourceSyncInferred,
			RouteRawPayload: payload,
		},
	}
	if err := dbpkg.GetDB().WithContext(ctx).Create(&rows).Error; err != nil {
		t.Fatalf("create site models failed: %v", err)
	}

	view, err := SiteChannelAccountGet(site.ID, account.ID, ctx)
	if err != nil {
		t.Fatalf("SiteChannelAccountGet failed: %v", err)
	}

	groupByKey := make(map[string]model.SiteChannelGroup)
	for _, group := range view.Groups {
		groupByKey[group.GroupKey] = group
	}

	defaultGroup := groupByKey["default"]
	if len(defaultGroup.Models) != 1 {
		t.Fatalf("expected default group to include one model, got %+v", defaultGroup.Models)
	}
	vipGroup := groupByKey["vip"]
	if len(vipGroup.Models) != 1 {
		t.Fatalf("expected vip group to include explicit model without keys, got %+v", vipGroup.Models)
	}
	if vipGroup.HasKeys {
		t.Fatalf("expected vip group to remain without keys")
	}
	if vipGroup.Models[0].ProjectedChannelID != nil {
		t.Fatalf("expected vip explicit model without keys not to have projected channel, got %+v", vipGroup.Models[0])
	}
}

func TestUpdateSiteProjectedKeysNormalizesPrefix(t *testing.T) {
	ctx := setupSiteOpTestDB(t)

	site := &model.Site{
		Name:     "site-channel-project-key-site",
		Platform: model.SitePlatformNewAPI,
		BaseURL:  "https://example.com",
		Enabled:  true,
	}
	if err := SiteCreate(site, ctx); err != nil {
		t.Fatalf("SiteCreate failed: %v", err)
	}

	account := &model.SiteAccount{
		SiteID:         site.ID,
		Name:           "site-channel-project-key-account",
		CredentialType: model.SiteCredentialTypeAccessToken,
		AccessToken:    "token",
		Enabled:        true,
	}
	if err := SiteAccountCreate(account, ctx); err != nil {
		t.Fatalf("SiteAccountCreate failed: %v", err)
	}

	channel := &model.Channel{
		Name:     "[Site] test / default",
		Type:     model.SiteModelRouteTypeOpenAIChat.ToOutboundType(),
		Enabled:  true,
		BaseUrls: []model.BaseUrl{{URL: "https://example.com/v1", Delay: 0}},
		Model:    "gpt-4o-mini",
		Keys: []model.ChannelKey{{
			Enabled:    true,
			ChannelKey: "legacy-key",
			Remark:     "default",
		}},
	}
	if err := ChannelCreate(channel, ctx); err != nil {
		t.Fatalf("ChannelCreate failed: %v", err)
	}

	binding := model.SiteChannelBinding{
		SiteID:        site.ID,
		SiteAccountID: account.ID,
		GroupKey:      model.SiteDefaultGroupKey,
		ChannelID:     channel.ID,
	}
	if err := dbpkg.GetDB().WithContext(ctx).Create(&binding).Error; err != nil {
		t.Fatalf("create site channel binding failed: %v", err)
	}

	reloaded, err := ChannelGet(channel.ID, ctx)
	if err != nil {
		t.Fatalf("ChannelGet failed: %v", err)
	}
	if len(reloaded.Keys) != 1 {
		t.Fatalf("expected existing channel to have one key, got %d", len(reloaded.Keys))
	}

	newRemark := "manual"
	newKey := "fresh-key"
	if err := UpdateSiteProjectedKeys(site.ID, account.ID, &model.SiteProjectedKeyUpdateRequest{
		GroupKey: model.SiteDefaultGroupKey,
		KeysToUpdate: []model.SiteProjectedKeyUpdateItem{{
			ID:         reloaded.Keys[0].ID,
			ChannelKey: &newKey,
			Remark:     &newRemark,
		}},
		KeysToAdd: []model.SiteProjectedKeyAddRequest{{
			Enabled:    true,
			ChannelKey: "backup-key",
			Remark:     "backup",
		}},
	}, ctx); err != nil {
		t.Fatalf("UpdateSiteProjectedKeys failed: %v", err)
	}

	var saved model.Channel
	if err := dbpkg.GetDB().WithContext(ctx).Preload("Keys", func(tx *gorm.DB) *gorm.DB {
		return tx.Order("id ASC")
	}).First(&saved, channel.ID).Error; err != nil {
		t.Fatalf("reload channel failed: %v", err)
	}
	if len(saved.Keys) != 2 {
		t.Fatalf("expected channel to have two keys after update, got %d", len(saved.Keys))
	}
	if saved.Keys[0].ChannelKey != "sk-fresh-key" {
		t.Fatalf("expected updated key to be normalized, got %q", saved.Keys[0].ChannelKey)
	}
	if saved.Keys[1].ChannelKey != "sk-backup-key" {
		t.Fatalf("expected added key to be normalized, got %q", saved.Keys[1].ChannelKey)
	}
}

func TestSiteChannelModelHistoryForAccountCountsRetryAttempts(t *testing.T) {
	site := model.Site{Platform: model.SitePlatformNewAPI}
	account := model.SiteAccount{
		ID: 1,
		ChannelBindings: []model.SiteChannelBinding{
			{SiteAccountID: 1, GroupKey: model.SiteDefaultGroupKey, ChannelID: 11},
			{SiteAccountID: 1, GroupKey: model.SiteDefaultGroupKey, ChannelID: 22},
		},
		Models: []model.SiteModel{
			{SiteAccountID: 1, GroupKey: model.SiteDefaultGroupKey, ModelName: "gpt-4o-mini", RouteType: model.SiteModelRouteTypeOpenAIChat},
			{SiteAccountID: 1, GroupKey: model.SiteDefaultGroupKey, ModelName: "claude-3-5-sonnet", RouteType: model.SiteModelRouteTypeAnthropic},
		},
	}

	history, err := siteChannelModelHistoryForAccount(site, account, []model.RelayLog{{
		Time:             200,
		RequestModelName: "gpt-4o",
		ActualModelName:  "claude-3-5-sonnet",
		Attempts: []model.ChannelAttempt{
			{ChannelID: 11, ChannelName: "channel-a", ModelName: "gpt-4o-mini", Status: model.AttemptSkipped, Msg: "no key"},
			{ChannelID: 11, ChannelName: "channel-a", ModelName: "gpt-4o-mini", Status: model.AttemptFailed, Msg: "429 upstream"},
			{ChannelID: 11, ChannelName: "channel-a", ModelName: "gpt-4o-mini", Status: model.AttemptFailed, Msg: "500 upstream"},
			{ChannelID: 22, ChannelName: "channel-b", ModelName: "claude-3-5-sonnet", Status: model.AttemptSuccess},
			{ChannelID: 22, ChannelName: "channel-b", ModelName: "claude-3-5-sonnet", Status: model.AttemptCircuitBreak, Msg: "breaker"},
		},
	}})
	if err != nil {
		t.Fatalf("siteChannelModelHistoryForAccount failed: %v", err)
	}

	chatKey := model.SiteDefaultGroupKey + "\x00gpt-4o-mini"
	chatSummary := history[chatKey]
	if chatSummary == nil {
		t.Fatalf("expected retry history for %q", chatKey)
	}
	if chatSummary.SuccessCount != 0 || chatSummary.FailureCount != 2 {
		t.Fatalf("expected chat retry counts 0/2, got success=%d failure=%d", chatSummary.SuccessCount, chatSummary.FailureCount)
	}
	if len(chatSummary.Recent) != 2 {
		t.Fatalf("expected 2 recent retry entries, got %d", len(chatSummary.Recent))
	}
	if chatSummary.Recent[0].Error != "500 upstream" || chatSummary.Recent[1].Error != "429 upstream" {
		t.Fatalf("expected retry entries in reverse attempt order, got %+v", chatSummary.Recent)
	}
	if chatSummary.Recent[0].RouteType != model.SiteModelRouteTypeOpenAIChat {
		t.Fatalf("expected retry route type %q, got %q", model.SiteModelRouteTypeOpenAIChat, chatSummary.Recent[0].RouteType)
	}

	anthropicKey := model.SiteDefaultGroupKey + "\x00claude-3-5-sonnet"
	anthropicSummary := history[anthropicKey]
	if anthropicSummary == nil {
		t.Fatalf("expected success history for %q", anthropicKey)
	}
	if anthropicSummary.SuccessCount != 1 || anthropicSummary.FailureCount != 0 {
		t.Fatalf("expected anthropic counts 1/0, got success=%d failure=%d", anthropicSummary.SuccessCount, anthropicSummary.FailureCount)
	}
	if len(anthropicSummary.Recent) != 1 {
		t.Fatalf("expected only one real success entry, got %d", len(anthropicSummary.Recent))
	}
	if anthropicSummary.Recent[0].ActualModel != "claude-3-5-sonnet" {
		t.Fatalf("expected success actual model to use attempt model, got %+v", anthropicSummary.Recent[0])
	}
}

func TestSiteChannelModelHistoryForAccountFallsBackForLegacyLogs(t *testing.T) {
	site := model.Site{Platform: model.SitePlatformNewAPI}
	account := model.SiteAccount{
		ID:              1,
		ChannelBindings: []model.SiteChannelBinding{{SiteAccountID: 1, GroupKey: model.SiteDefaultGroupKey, ChannelID: 11}},
		Models:          []model.SiteModel{{SiteAccountID: 1, GroupKey: model.SiteDefaultGroupKey, ModelName: "gpt-4o-mini", RouteType: model.SiteModelRouteTypeOpenAIChat}},
	}

	history, err := siteChannelModelHistoryForAccount(site, account, []model.RelayLog{{
		Time:             300,
		RequestModelName: "gpt-4o",
		ChannelId:        11,
		ChannelName:      "channel-a",
		ActualModelName:  "gpt-4o-mini",
		Error:            "legacy failure",
	}})
	if err != nil {
		t.Fatalf("siteChannelModelHistoryForAccount failed: %v", err)
	}

	key := model.SiteDefaultGroupKey + "\x00gpt-4o-mini"
	summary := history[key]
	if summary == nil {
		t.Fatalf("expected legacy history for %q", key)
	}
	if summary.SuccessCount != 0 || summary.FailureCount != 1 {
		t.Fatalf("expected legacy counts 0/1, got success=%d failure=%d", summary.SuccessCount, summary.FailureCount)
	}
	if len(summary.Recent) != 1 {
		t.Fatalf("expected one legacy recent entry, got %d", len(summary.Recent))
	}
	if summary.Recent[0].Error != "legacy failure" || summary.Recent[0].ChannelID != 11 {
		t.Fatalf("unexpected legacy recent entry: %+v", summary.Recent[0])
	}
}
