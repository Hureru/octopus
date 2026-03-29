package model

import "testing"

func TestInferSiteModelRouteType(t *testing.T) {
	tests := []struct {
		name      string
		modelName string
		expected  SiteModelRouteType
	}{
		{name: "anthropic models stay anthropic", modelName: "claude-3-5-sonnet", expected: SiteModelRouteTypeAnthropic},
		{name: "gemini models stay gemini", modelName: "gemini-2.0-flash", expected: SiteModelRouteTypeGemini},
		{name: "embedding models use embedding route", modelName: "text-embedding-3-large", expected: SiteModelRouteTypeOpenAIEmbedding},
		{name: "gpt 4o defaults to chat without metadata", modelName: "gpt-4o-mini", expected: SiteModelRouteTypeOpenAIChat},
		{name: "gpt 4.1 defaults to chat without metadata", modelName: "gpt-4.1", expected: SiteModelRouteTypeOpenAIChat},
		{name: "gpt 5 defaults to chat without metadata", modelName: "gpt-5-mini", expected: SiteModelRouteTypeOpenAIChat},
		{name: "o series defaults to chat without metadata", modelName: "o3-mini", expected: SiteModelRouteTypeOpenAIChat},
		{name: "older openai chat models remain chat", modelName: "gpt-4-turbo", expected: SiteModelRouteTypeOpenAIChat},
		{name: "generic compat models remain chat", modelName: "deepseek-chat", expected: SiteModelRouteTypeOpenAIChat},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if actual := InferSiteModelRouteType(tt.modelName); actual != tt.expected {
				t.Fatalf("expected %q, got %q", tt.expected, actual)
			}
		})
	}
}
