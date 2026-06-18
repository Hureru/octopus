package sitesync

import (
	"testing"

	dbpkg "github.com/bestruirui/octopus/internal/db"
	"github.com/bestruirui/octopus/internal/model"
	"github.com/bestruirui/octopus/internal/op"
	"github.com/bestruirui/octopus/internal/transformer/outbound"
)

func TestShouldSplitByOutboundTypeForcesSplitWhenRouteBaseURLsConfigured(t *testing.T) {
	tests := []struct {
		name           string
		site           *model.Site
		expectedSplit  bool
		expectedReason string
	}{
		{
			name: "api platform without route overrides - no split",
			site: &model.Site{
				Platform:      model.SitePlatformAPI,
				BaseURL:       "https://api.openai.com",
				RouteBaseURLs: []model.SiteRouteBaseURL{},
			},
			expectedSplit:  false,
			expectedReason: "api platform defaults to no split",
		},
		{
			name: "api platform with route overrides - forces split",
			site: &model.Site{
				Platform: model.SitePlatformAPI,
				BaseURL:  "https://gateway.example.com",
				RouteBaseURLs: []model.SiteRouteBaseURL{
					{RouteType: model.SiteModelRouteTypeAnthropic, BaseURL: "https://gateway.example.com/anthropic/v1"},
					{RouteType: model.SiteModelRouteTypeOpenAIChat, BaseURL: "https://gateway.example.com/openai/v1"},
				},
			},
			expectedSplit:  true,
			expectedReason: "route overrides force split even on api platform",
		},
		{
			name: "api platform with single route override - forces split",
			site: &model.Site{
				Platform: model.SitePlatformAPI,
				BaseURL:  "https://gateway.example.com",
				RouteBaseURLs: []model.SiteRouteBaseURL{
					{RouteType: model.SiteModelRouteTypeAnthropic, BaseURL: "https://gateway.example.com/anthropic/v1"},
				},
			},
			expectedSplit:  true,
			expectedReason: "route overrides force split even on api platform",
		},
		{
			name: "api platform without route overrides (gemini default) - no split",
			site: &model.Site{
				Platform:         model.SitePlatformAPI,
				DefaultRouteType: model.SiteModelRouteTypeGemini,
				BaseURL:          "https://gemini.example.com",
				RouteBaseURLs:    []model.SiteRouteBaseURL{},
			},
			expectedSplit:  false,
			expectedReason: "api platform defaults to no split",
		},
		{
			name: "new-api platform without route overrides - splits by default",
			site: &model.Site{
				Platform:      model.SitePlatformNewAPI,
				BaseURL:       "https://newapi.example.com",
				RouteBaseURLs: []model.SiteRouteBaseURL{},
			},
			expectedSplit:  true,
			expectedReason: "new-api family platforms split by default",
		},
		{
			name: "new-api platform with route overrides - continues to split",
			site: &model.Site{
				Platform: model.SitePlatformNewAPI,
				BaseURL:  "https://newapi.example.com",
				RouteBaseURLs: []model.SiteRouteBaseURL{
					{RouteType: model.SiteModelRouteTypeAnthropic, BaseURL: "https://newapi.example.com/anthropic/v1"},
				},
			},
			expectedSplit:  true,
			expectedReason: "route overrides reinforce default split behavior",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			actual := shouldSplitByOutboundType(tt.site)
			if actual != tt.expectedSplit {
				t.Errorf("shouldSplitByOutboundType() = %v, want %v (reason: %s)", actual, tt.expectedSplit, tt.expectedReason)
			}
		})
	}
}

func TestProjectAccountWithRouteOverridesOnOpenAIPlatform(t *testing.T) {
	ctx := setupProjectTestDB(t)

	site := &model.Site{
		Name:     "API Gateway",
		Platform: model.SitePlatformAPI,
		BaseURL:  "https://gateway.example.com",
		Enabled:  true,
		RouteBaseURLs: []model.SiteRouteBaseURL{
			{RouteType: model.SiteModelRouteTypeAnthropic, BaseURL: "https://gateway.example.com/anthropic/v1"},
			{RouteType: model.SiteModelRouteTypeOpenAIChat, BaseURL: "https://gateway.example.com/openai/v1"},
		},
	}
	if err := op.SiteCreate(site, ctx); err != nil {
		t.Fatalf("SiteCreate failed: %v", err)
	}

	account := &model.SiteAccount{
		SiteID:         site.ID,
		Name:           "API Key Account",
		CredentialType: model.SiteCredentialTypeAPIKey,
		APIKey:         "sk-test-key-12345",
		Enabled:        true,
		AutoSync:       false,
		AutoCheckin:    false,
	}
	if err := op.SiteAccountCreate(account, ctx); err != nil {
		t.Fatalf("SiteAccountCreate failed: %v", err)
	}

	tokens := []model.SiteToken{
		{SiteAccountID: account.ID, Name: "main", Token: "sk-test-key-12345", GroupKey: "default", GroupName: "default", Enabled: true, ValueStatus: model.SiteTokenValueStatusReady},
	}
	if err := dbpkg.GetDB().WithContext(ctx).Create(&tokens).Error; err != nil {
		t.Fatalf("create site tokens failed: %v", err)
	}

	models := []model.SiteModel{
		{SiteAccountID: account.ID, GroupKey: "default", ModelName: "claude-3-5-sonnet-20241022", RouteType: model.SiteModelRouteTypeAnthropic, Disabled: false},
		{SiteAccountID: account.ID, GroupKey: "default", ModelName: "gpt-4o", RouteType: model.SiteModelRouteTypeOpenAIChat, Disabled: false},
	}
	if err := dbpkg.GetDB().WithContext(ctx).Create(&models).Error; err != nil {
		t.Fatalf("create site models failed: %v", err)
	}

	channelIDs, err := ProjectAccount(ctx, account.ID)
	if err != nil {
		t.Fatalf("ProjectAccount returned error: %v", err)
	}

	if len(channelIDs) != 2 {
		t.Fatalf("expected 2 managed channels (split by route type due to RouteBaseURLs), got %d", len(channelIDs))
	}

	channelsByGroup := loadProjectedChannelsByGroupKey(t, ctx, account.ID)
	if len(channelsByGroup) != 2 {
		t.Fatalf("expected 2 bindings, got %d", len(channelsByGroup))
	}

	// Verify anthropic channel uses the anthropic base URL override
	anthropicChannel, ok := channelsByGroup["default::anthropic"]
	if !ok {
		t.Fatalf("expected anthropic channel binding, not found")
	}
	if anthropicChannel.Type != outbound.OutboundTypeAnthropic {
		t.Errorf("expected anthropic channel type, got %v", anthropicChannel.Type)
	}
	if len(anthropicChannel.BaseUrls) == 0 || anthropicChannel.BaseUrls[0].URL != "https://gateway.example.com/anthropic/v1" {
		t.Errorf("expected anthropic base URL override, got %v", anthropicChannel.BaseUrls)
	}
	if anthropicChannel.Model != "claude-3-5-sonnet-20241022" {
		t.Errorf("expected claude model, got %s", anthropicChannel.Model)
	}

	// Verify openai_chat channel uses the openai_chat base URL override
	openaiChannel, ok := channelsByGroup["default"]
	if !ok {
		t.Fatalf("expected openai_chat channel binding, not found")
	}
	if openaiChannel.Type != outbound.OutboundTypeOpenAIChat {
		t.Errorf("expected openai_chat channel type, got %v", openaiChannel.Type)
	}
	if len(openaiChannel.BaseUrls) == 0 || openaiChannel.BaseUrls[0].URL != "https://gateway.example.com/openai/v1" {
		t.Errorf("expected openai_chat base URL override, got %v", openaiChannel.BaseUrls)
	}
	if openaiChannel.Model != "gpt-4o" {
		t.Errorf("expected gpt model, got %s", openaiChannel.Model)
	}

	// Verify keys are not prefixed (api platform keeps verbatim)
	if len(openaiChannel.Keys) == 0 || openaiChannel.Keys[0].ChannelKey != "sk-test-key-12345" {
		t.Errorf("expected verbatim key for api platform, got %v", openaiChannel.Keys)
	}
	if len(anthropicChannel.Keys) == 0 || anthropicChannel.Keys[0].ChannelKey != "sk-test-key-12345" {
		t.Errorf("expected verbatim key for api platform, got %v", anthropicChannel.Keys)
	}
}
