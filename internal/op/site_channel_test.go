package op

import (
	"testing"

	dbpkg "github.com/bestruirui/octopus/internal/db"
	"github.com/bestruirui/octopus/internal/model"
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
