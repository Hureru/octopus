package sitesync

import (
	"testing"

	"github.com/bestruirui/octopus/internal/model"
)

func TestShouldSplitForAccount(t *testing.T) {
	tests := []struct {
		name     string
		site     *model.Site
		account  *model.SiteAccount
		expected bool
		reason   string
	}{
		{
			name: "API platform with RouteBaseURLs forces split",
			site: &model.Site{
				Platform: model.SitePlatformAPI,
				RouteBaseURLs: []model.SiteRouteBaseURL{
					{RouteType: model.SiteModelRouteTypeOpenAIChat, BaseURL: "https://example.com/v1"},
					{RouteType: model.SiteModelRouteTypeAnthropic, BaseURL: "https://example.com/claude"},
				},
			},
			account: &model.SiteAccount{
				Models: []model.SiteModel{
					{ModelName: "gpt-4", RouteType: model.SiteModelRouteTypeOpenAIChat, ManualOverride: false},
				},
			},
			expected: true,
			reason:   "RouteBaseURLs配置强制拆分",
		},
		{
			name: "NewAPI platform splits by default",
			site: &model.Site{
				Platform: model.SitePlatformNewAPI,
			},
			account: &model.SiteAccount{
				Models: []model.SiteModel{
					{ModelName: "gpt-4", RouteType: model.SiteModelRouteTypeOpenAIChat, ManualOverride: false},
				},
			},
			expected: true,
			reason:   "NewAPI平台默认拆分",
		},
		{
			name: "API platform with single auto-inferred route type - no split",
			site: &model.Site{
				Platform: model.SitePlatformAPI,
			},
			account: &model.SiteAccount{
				Models: []model.SiteModel{
					{ModelName: "gpt-4", RouteType: model.SiteModelRouteTypeOpenAIChat, ManualOverride: false, Disabled: false},
					{ModelName: "gpt-3.5-turbo", RouteType: model.SiteModelRouteTypeOpenAIChat, ManualOverride: false, Disabled: false},
				},
			},
			expected: false,
			reason:   "所有模型都是自动推断的同一类型，不需要拆分",
		},
		{
			name: "API platform with single manual override route type - no split",
			site: &model.Site{
				Platform: model.SitePlatformAPI,
			},
			account: &model.SiteAccount{
				Models: []model.SiteModel{
					{ModelName: "gpt-4", RouteType: model.SiteModelRouteTypeOpenAIChat, ManualOverride: true, Disabled: false},
					{ModelName: "gpt-3.5-turbo", RouteType: model.SiteModelRouteTypeOpenAIChat, ManualOverride: true, Disabled: false},
				},
			},
			expected: false,
			reason:   "所有手动覆盖都是同一类型，不需要拆分",
		},
		{
			name: "API platform with mixed manual override route types - auto split",
			site: &model.Site{
				Platform: model.SitePlatformAPI,
			},
			account: &model.SiteAccount{
				Models: []model.SiteModel{
					{ModelName: "gpt-4", RouteType: model.SiteModelRouteTypeOpenAIChat, ManualOverride: true, Disabled: false},
					{ModelName: "claude-3-5-sonnet", RouteType: model.SiteModelRouteTypeAnthropic, ManualOverride: true, Disabled: false},
				},
			},
			expected: true,
			reason:   "检测到混合的手动覆盖类型，自动启用拆分",
		},
		{
			name: "API platform with single manual override differing from default - auto split",
			site: &model.Site{
				Platform: model.SitePlatformAPI,
			},
			account: &model.SiteAccount{
				Models: []model.SiteModel{
					{ModelName: "gpt-4", RouteType: model.SiteModelRouteTypeOpenAIChat, ManualOverride: false, Disabled: false},
					{ModelName: "claude-3-5-sonnet", RouteType: model.SiteModelRouteTypeAnthropic, ManualOverride: true, Disabled: false},
				},
			},
			expected: true,
			reason:   "单个手动覆盖与平台默认不同，触发拆分",
		},
		{
			name: "API platform with single manual override matching default - no split",
			site: &model.Site{
				Platform: model.SitePlatformAPI,
			},
			account: &model.SiteAccount{
				Models: []model.SiteModel{
					{ModelName: "gpt-4", RouteType: model.SiteModelRouteTypeOpenAIChat, ManualOverride: true, Disabled: false},
					{ModelName: "gpt-3.5-turbo", RouteType: model.SiteModelRouteTypeOpenAIChat, ManualOverride: false, Disabled: false},
				},
			},
			expected: false,
			reason:   "手动覆盖与平台默认相同，不需要拆分",
		},
		{
			name: "API platform with disabled models ignored",
			site: &model.Site{
				Platform: model.SitePlatformAPI,
			},
			account: &model.SiteAccount{
				Models: []model.SiteModel{
					{ModelName: "gpt-4", RouteType: model.SiteModelRouteTypeOpenAIChat, ManualOverride: true, Disabled: false},
					{ModelName: "claude-3-5-sonnet", RouteType: model.SiteModelRouteTypeAnthropic, ManualOverride: true, Disabled: true},
				},
			},
			expected: false,
			reason:   "禁用的模型不计入拆分判断",
		},
		{
			name: "API platform with three manual override types - auto split",
			site: &model.Site{
				Platform: model.SitePlatformAPI,
			},
			account: &model.SiteAccount{
				Models: []model.SiteModel{
					{ModelName: "gpt-4", RouteType: model.SiteModelRouteTypeOpenAIChat, ManualOverride: true, Disabled: false},
					{ModelName: "claude-3-5-sonnet", RouteType: model.SiteModelRouteTypeAnthropic, ManualOverride: true, Disabled: false},
					{ModelName: "gemini-pro", RouteType: model.SiteModelRouteTypeGemini, ManualOverride: true, Disabled: false},
				},
			},
			expected: true,
			reason:   "检测到三种手动覆盖类型，自动启用拆分",
		},
		{
			name: "API platform with no models - no split",
			site: &model.Site{
				Platform: model.SitePlatformAPI,
			},
			account: &model.SiteAccount{
				Models: []model.SiteModel{},
			},
			expected: false,
			reason:   "没有模型，不需要拆分",
		},
		{
			name: "API platform with non-projected route types ignored",
			site: &model.Site{
				Platform: model.SitePlatformAPI,
			},
			account: &model.SiteAccount{
				Models: []model.SiteModel{
					{ModelName: "gpt-4", RouteType: model.SiteModelRouteTypeOpenAIChat, ManualOverride: true, Disabled: false},
					{ModelName: "unknown-model", RouteType: model.SiteModelRouteTypeUnknown, ManualOverride: true, Disabled: false},
				},
			},
			expected: false,
			reason:   "非投影类型不计入拆分判断",
		},
		{
			name:     "nil site returns false",
			site:     nil,
			account:  &model.SiteAccount{Models: []model.SiteModel{{ModelName: "gpt-4", RouteType: model.SiteModelRouteTypeOpenAIChat, ManualOverride: true}}},
			expected: false,
			reason:   "nil site 安全返回 false",
		},
		{
			name:     "nil account returns false",
			site:     &model.Site{Platform: model.SitePlatformAPI},
			account:  nil,
			expected: false,
			reason:   "nil account 安全返回 false",
		},
		{
			name:     "both nil returns false",
			site:     nil,
			account:  nil,
			expected: false,
			reason:   "双 nil 安全返回 false",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := shouldSplitForAccount(tt.account, tt.site)
			if result != tt.expected {
				t.Errorf("shouldSplitForAccount() = %v, expected %v (reason: %s)", result, tt.expected, tt.reason)
			}
		})
	}
}
