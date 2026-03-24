package sitesync

import (
	"testing"

	"github.com/bestruirui/octopus/internal/model"
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
