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

func TestBuildSiteModelRouteDetectionAddsHeuristicResponsesForGPT5(t *testing.T) {
	detection, ok := buildSiteModelRouteDetection(
		"gpt-5.4",
		nil,
		[]string{"/v1/chat/completions"},
		"/api/pricing",
		map[string]struct{}{"gpt-5.4": {}},
	)
	if !ok {
		t.Fatalf("expected heuristic response detection to be produced")
	}
	if !detection.ApplyRouteType {
		t.Fatalf("expected heuristic response detection to apply route type")
	}

	metadata, ok := model.ParseSiteModelRouteMetadata(detection.RouteRawPayload)
	if !ok {
		t.Fatalf("expected route metadata to parse")
	}
	if metadata.RouteType != model.SiteModelRouteTypeOpenAIResponse {
		t.Fatalf("expected heuristic detection route type %q, got %q", model.SiteModelRouteTypeOpenAIResponse, metadata.RouteType)
	}
	if len(metadata.SupportedEndpointTypes) != 1 || metadata.SupportedEndpointTypes[0] != "/v1/chat/completions" {
		t.Fatalf("expected upstream endpoint list to remain intact, got %#v", metadata.SupportedEndpointTypes)
	}
	if len(metadata.HeuristicEndpointTypes) != 1 || metadata.HeuristicEndpointTypes[0] != "/v1/responses" {
		t.Fatalf("expected heuristic endpoint list to record injected response support, got %#v", metadata.HeuristicEndpointTypes)
	}
	if len(metadata.NormalizedEndpointTypes) != 2 {
		t.Fatalf("expected normalized endpoint list to include explicit and heuristic routes, got %#v", metadata.NormalizedEndpointTypes)
	}
}
