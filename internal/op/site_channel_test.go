package op

import (
	"testing"

	dbpkg "github.com/bestruirui/octopus/internal/db"
	"github.com/bestruirui/octopus/internal/model"
	"github.com/bestruirui/octopus/internal/transformer/outbound"
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

func TestUpdateSiteSourceKeysMarksExistingTokenAsManual(t *testing.T) {
	ctx := setupSiteOpTestDB(t)

	site := &model.Site{
		Name:     "site-channel-source-manual-site",
		Platform: model.SitePlatformNewAPI,
		BaseURL:  "https://example.com",
		Enabled:  true,
	}
	if err := SiteCreate(site, ctx); err != nil {
		t.Fatalf("SiteCreate failed: %v", err)
	}

	account := &model.SiteAccount{
		SiteID:         site.ID,
		Name:           "site-channel-source-manual-account",
		CredentialType: model.SiteCredentialTypeAccessToken,
		AccessToken:    "token",
		Enabled:        true,
	}
	if err := SiteAccountCreate(account, ctx); err != nil {
		t.Fatalf("SiteAccountCreate failed: %v", err)
	}

	row := model.SiteToken{
		SiteAccountID: account.ID,
		Name:          "legacy",
		Token:         "sk-legacy-key",
		GroupKey:      model.SiteDefaultGroupKey,
		GroupName:     model.SiteDefaultGroupName,
		Enabled:       true,
		ValueStatus:   model.SiteTokenValueStatusReady,
		Source:        "sync",
	}
	if err := dbpkg.GetDB().WithContext(ctx).Create(&row).Error; err != nil {
		t.Fatalf("create site token failed: %v", err)
	}

	updated := "new-manual-key"
	if err := UpdateSiteSourceKeys(site.ID, account.ID, &model.SiteSourceKeyUpdateRequest{
		GroupKey: model.SiteDefaultGroupKey,
		KeysToUpdate: []model.SiteSourceKeyUpdateItem{{
			ID:    row.ID,
			Token: &updated,
		}},
	}, ctx); err != nil {
		t.Fatalf("UpdateSiteSourceKeys failed: %v", err)
	}

	var saved model.SiteToken
	if err := dbpkg.GetDB().WithContext(ctx).First(&saved, row.ID).Error; err != nil {
		t.Fatalf("reload site token failed: %v", err)
	}
	if saved.Source != "manual" {
		t.Fatalf("expected updated token source to become manual, got %q", saved.Source)
	}
	if saved.Token != "sk-new-manual-key" {
		t.Fatalf("expected updated token to be normalized, got %q", saved.Token)
	}
	if saved.ValueStatus != model.SiteTokenValueStatusReady {
		t.Fatalf("expected updated token value_status to be ready, got %q", saved.ValueStatus)
	}
}

func TestUpdateSiteSourceKeysRestoresReadyWhenMaskedPendingTokenIsCompleted(t *testing.T) {
	ctx := setupSiteOpTestDB(t)

	site := &model.Site{
		Name:     "site-channel-source-restore-ready-site",
		Platform: model.SitePlatformNewAPI,
		BaseURL:  "https://example.com",
		Enabled:  true,
	}
	if err := SiteCreate(site, ctx); err != nil {
		t.Fatalf("SiteCreate failed: %v", err)
	}

	account := &model.SiteAccount{
		SiteID:         site.ID,
		Name:           "site-channel-source-restore-ready-account",
		CredentialType: model.SiteCredentialTypeAccessToken,
		AccessToken:    "token",
		Enabled:        true,
	}
	if err := SiteAccountCreate(account, ctx); err != nil {
		t.Fatalf("SiteAccountCreate failed: %v", err)
	}

	row := model.SiteToken{
		SiteAccountID: account.ID,
		Name:          "legacy",
		Token:         "yzFy**********OTkb",
		GroupKey:      model.SiteDefaultGroupKey,
		GroupName:     model.SiteDefaultGroupName,
		Enabled:       false,
		ValueStatus:   model.SiteTokenValueStatusMaskedPending,
		Source:        "sync",
	}
	if err := dbpkg.GetDB().WithContext(ctx).Create(&row).Error; err != nil {
		t.Fatalf("create site token failed: %v", err)
	}

	updated := "yzFyREALREALOTkb"
	if err := UpdateSiteSourceKeys(site.ID, account.ID, &model.SiteSourceKeyUpdateRequest{
		GroupKey: model.SiteDefaultGroupKey,
		KeysToUpdate: []model.SiteSourceKeyUpdateItem{{
			ID:    row.ID,
			Token: &updated,
		}},
	}, ctx); err != nil {
		t.Fatalf("UpdateSiteSourceKeys failed: %v", err)
	}

	var saved model.SiteToken
	if err := dbpkg.GetDB().WithContext(ctx).First(&saved, row.ID).Error; err != nil {
		t.Fatalf("reload site token failed: %v", err)
	}
	if saved.Token != "sk-yzFyREALREALOTkb" {
		t.Fatalf("expected completed token to be normalized and saved, got %q", saved.Token)
	}
	if saved.ValueStatus != model.SiteTokenValueStatusReady {
		t.Fatalf("expected completed token value_status to restore ready, got %q", saved.ValueStatus)
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
		Model:     "gpt-4o-mini",
		AutoSync:  false,
		AutoGroup: model.AutoGroupTypeNone,
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

func TestSiteChannelAccountGetCountsMaskedPendingKeysAsPendingOnly(t *testing.T) {
	ctx := setupSiteOpTestDB(t)

	site := &model.Site{
		Name:     "site-channel-masked-pending-site",
		Platform: model.SitePlatformNewAPI,
		BaseURL:  "https://example.com",
		Enabled:  true,
	}
	if err := SiteCreate(site, ctx); err != nil {
		t.Fatalf("SiteCreate failed: %v", err)
	}

	account := &model.SiteAccount{
		SiteID:         site.ID,
		Name:           "site-channel-masked-pending-account",
		CredentialType: model.SiteCredentialTypeAccessToken,
		AccessToken:    "token",
		Enabled:        true,
	}
	if err := SiteAccountCreate(account, ctx); err != nil {
		t.Fatalf("SiteAccountCreate failed: %v", err)
	}

	if err := dbpkg.GetDB().WithContext(ctx).Create(&model.SiteUserGroup{
		SiteAccountID: account.ID,
		GroupKey:      model.SiteDefaultGroupKey,
		Name:          model.SiteDefaultGroupName,
	}).Error; err != nil {
		t.Fatalf("create site group failed: %v", err)
	}

	if err := dbpkg.GetDB().WithContext(ctx).Create(&model.SiteToken{
		SiteAccountID: account.ID,
		Name:          "masked-only",
		Token:         "sk-ab***xyz",
		ValueStatus:   model.SiteTokenValueStatusMaskedPending,
		GroupKey:      model.SiteDefaultGroupKey,
		GroupName:     model.SiteDefaultGroupName,
		Enabled:       false,
	}).Error; err != nil {
		t.Fatalf("create site token failed: %v", err)
	}

	view, err := SiteChannelAccountGet(site.ID, account.ID, ctx)
	if err != nil {
		t.Fatalf("SiteChannelAccountGet failed: %v", err)
	}
	if len(view.Groups) != 1 {
		t.Fatalf("expected one group, got %+v", view.Groups)
	}
	group := view.Groups[0]
	if group.KeyCount != 1 {
		t.Fatalf("expected key_count=1, got %d", group.KeyCount)
	}
	if group.EnabledKeyCount != 0 {
		t.Fatalf("expected enabled_key_count=0 for masked_pending token, got %d", group.EnabledKeyCount)
	}
	if group.MaskedPendingKeyCount != 1 {
		t.Fatalf("expected masked_pending_key_count=1, got %d", group.MaskedPendingKeyCount)
	}
}

func TestUpdateSiteSourceKeysNormalizesPrefix(t *testing.T) {
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

	existing := model.SiteToken{
		SiteAccountID: account.ID,
		Name:          "legacy",
		Token:         "sk-legacy-key",
		GroupKey:      model.SiteDefaultGroupKey,
		GroupName:     model.SiteDefaultGroupName,
		Enabled:       true,
		ValueStatus:   model.SiteTokenValueStatusReady,
	}
	if err := dbpkg.GetDB().WithContext(ctx).Create(&existing).Error; err != nil {
		t.Fatalf("create site token failed: %v", err)
	}

	newName := "manual"
	newKey := "fresh-key"
	if err := UpdateSiteSourceKeys(site.ID, account.ID, &model.SiteSourceKeyUpdateRequest{
		GroupKey: model.SiteDefaultGroupKey,
		KeysToUpdate: []model.SiteSourceKeyUpdateItem{{
			ID:    existing.ID,
			Token: &newKey,
			Name:  &newName,
		}},
		KeysToAdd: []model.SiteSourceKeyAddRequest{{
			Enabled: true,
			Token:   "backup-key",
			Name:    "backup",
		}},
	}, ctx); err != nil {
		t.Fatalf("UpdateSiteSourceKeys failed: %v", err)
	}

	var saved []model.SiteToken
	if err := dbpkg.GetDB().WithContext(ctx).
		Where("site_account_id = ? AND group_key = ?", account.ID, model.SiteDefaultGroupKey).
		Order("id ASC").
		Find(&saved).Error; err != nil {
		t.Fatalf("reload site tokens failed: %v", err)
	}
	if len(saved) != 2 {
		t.Fatalf("expected account to have two site tokens after update, got %d", len(saved))
	}
	if saved[0].Token != "sk-fresh-key" {
		t.Fatalf("expected updated token to be normalized, got %q", saved[0].Token)
	}
	if saved[0].Name != "manual" {
		t.Fatalf("expected updated name to be saved, got %q", saved[0].Name)
	}
	if saved[1].Token != "sk-backup-key" {
		t.Fatalf("expected added token to be normalized, got %q", saved[1].Token)
	}
}

func TestSiteChannelHistoryBucketingChoosesAdaptiveSpan(t *testing.T) {
	hour := int64(3600)
	day := 24 * hour
	cases := []struct {
		spanSeconds int64
		want        int
	}{
		{spanSeconds: hour, want: int(hour)},
		{spanSeconds: 24 * hour, want: int(hour)},
		{spanSeconds: 7 * day, want: int(6 * hour)},
		{spanSeconds: 30 * day, want: int(day)},
		{spanSeconds: 90 * day, want: int(7 * day)},
	}
	for _, c := range cases {
		got := chooseBucketSpan(c.spanSeconds)
		if got != c.want {
			t.Errorf("chooseBucketSpan(%d) = %d, want %d", c.spanSeconds, got, c.want)
		}
	}
}

func TestBuildSiteModelSummaryAggregatesAndBuckets(t *testing.T) {
	hourBase := 1_000_000
	hours := []model.StatsSiteModelHourly{
		{Hour: hourBase, GroupKey: "g", ModelName: "m", StatsMetrics: model.StatsMetrics{RequestSuccess: 3, RequestFailed: 1}},
		{Hour: hourBase + 1, GroupKey: "g", ModelName: "m", StatsMetrics: model.StatsMetrics{RequestSuccess: 2, RequestFailed: 0}},
		{Hour: hourBase + 5, GroupKey: "g", ModelName: "m", StatsMetrics: model.StatsMetrics{RequestSuccess: 0, RequestFailed: 4}},
	}
	summary := buildSiteModelSummary(hours)
	if summary.SuccessCount != 5 || summary.FailureCount != 5 {
		t.Fatalf("expected counts 5/5, got %d/%d", summary.SuccessCount, summary.FailureCount)
	}
	if summary.BucketSpan != 3600 {
		t.Fatalf("expected 1h bucket for short span, got %d", summary.BucketSpan)
	}
	if len(summary.Buckets) != 3 {
		t.Fatalf("expected 3 hourly buckets, got %d", len(summary.Buckets))
	}
	if summary.Buckets[0].Success != 3 || summary.Buckets[0].Failure != 1 {
		t.Fatalf("first bucket mismatch: %+v", summary.Buckets[0])
	}
	if summary.LastRequestAt == nil || *summary.LastRequestAt < int64(hourBase+5)*3600 {
		t.Fatalf("expected LastRequestAt to reflect latest hour, got %v", summary.LastRequestAt)
	}
}
