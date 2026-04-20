package openai

import "testing"

func TestValidateReasoningEffort(t *testing.T) {
	cases := map[string]string{
		"minimal":     "minimal",
		"low":         "low",
		"medium":      "medium",
		"high":        "high",
		"":            "",
		"turbo":       "",
		"ultra":       "",
		"MEDIUM":      "", // case-sensitive whitelist
		" low":        "",
	}
	for in, want := range cases {
		if got := validateReasoningEffort(in); got != want {
			t.Errorf("validateReasoningEffort(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestValidateReasoningSummary(t *testing.T) {
	cases := map[string]string{
		"auto":     "auto",
		"concise":  "concise",
		"detailed": "detailed",
		"":         "",
		"brief":    "",
		"verbose":  "",
		"Auto":     "",
	}
	for in, want := range cases {
		if got := validateReasoningSummary(in); got != want {
			t.Errorf("validateReasoningSummary(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestResponsesTerminalEvent(t *testing.T) {
	cases := []struct {
		finish      string
		wantEvent   string
		wantStatus  string
	}{
		{"stop", "response.completed", "completed"},
		{"tool_calls", "response.completed", "completed"},
		{"length", "response.incomplete", "incomplete"},
		{"pause_turn", "response.incomplete", "incomplete"},
		{"error", "response.failed", "failed"},
		{"malformed_function_call", "response.failed", "failed"},
		{"safety", "response.failed", "failed"},
		{"recitation", "response.failed", "failed"},
		{"content_filter", "response.failed", "failed"},
		{"refusal", "response.failed", "failed"},
		{"prohibited_content", "response.failed", "failed"},
		{"spii", "response.failed", "failed"},
		{"image_safety", "response.failed", "failed"},
		{"", "response.completed", "completed"},
	}
	for _, tc := range cases {
		gotEvent, gotStatus := responsesTerminalEvent(tc.finish)
		if gotEvent != tc.wantEvent || gotStatus != tc.wantStatus {
			t.Errorf("responsesTerminalEvent(%q) = (%q, %q), want (%q, %q)",
				tc.finish, gotEvent, gotStatus, tc.wantEvent, tc.wantStatus)
		}
	}
}
