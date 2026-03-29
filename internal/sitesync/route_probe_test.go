package sitesync

import (
	"testing"

	"github.com/bestruirui/octopus/internal/model"
)

func TestPickPreferredDetectedRouteType(t *testing.T) {
	tests := []struct {
		name      string
		modelName string
		values    []model.SiteModelRouteType
		expected  model.SiteModelRouteType
	}{
		{
			name:      "claude prefers anthropic when available",
			modelName: "claude-3-5-sonnet",
			values:    []model.SiteModelRouteType{model.SiteModelRouteTypeOpenAIChat, model.SiteModelRouteTypeAnthropic},
			expected:  model.SiteModelRouteTypeAnthropic,
		},
		{
			name:      "claude falls back to response before chat",
			modelName: "claude-3-5-sonnet",
			values:    []model.SiteModelRouteType{model.SiteModelRouteTypeOpenAIChat, model.SiteModelRouteTypeOpenAIResponse},
			expected:  model.SiteModelRouteTypeOpenAIResponse,
		},
		{
			name:      "claude falls back to chat before gemini when anthropic missing",
			modelName: "claude-3-5-sonnet",
			values:    []model.SiteModelRouteType{model.SiteModelRouteTypeGemini, model.SiteModelRouteTypeOpenAIChat},
			expected:  model.SiteModelRouteTypeOpenAIChat,
		},
		{
			name:      "gemini keeps native route when available",
			modelName: "gemini-2.0-flash",
			values:    []model.SiteModelRouteType{model.SiteModelRouteTypeOpenAIChat, model.SiteModelRouteTypeGemini},
			expected:  model.SiteModelRouteTypeGemini,
		},
		{
			name:      "gpt prefers response over chat",
			modelName: "gpt-4o-mini",
			values:    []model.SiteModelRouteType{model.SiteModelRouteTypeOpenAIChat, model.SiteModelRouteTypeOpenAIResponse},
			expected:  model.SiteModelRouteTypeOpenAIResponse,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if actual := pickPreferredDetectedRouteType(tt.modelName, tt.values); actual != tt.expected {
				t.Fatalf("expected %q, got %q", tt.expected, actual)
			}
		})
	}
}
