package sitesync

import (
	"strings"
	"testing"
	"unicode"
	"unicode/utf8"
)

func TestEmbeddedHTMLSummaryForStatusSanitizesPrefix(t *testing.T) {
	message := "request failed api_key=secret-value\x00\n<html><title>Upstream Error</title></html>"

	summary := embeddedHTMLSummaryForStatus(message)

	if strings.Contains(summary, "secret-value") {
		t.Fatalf("summary leaked secret: %q", summary)
	}
	if containsDisallowedControl(summary) {
		t.Fatalf("summary contains control character: %q", summary)
	}
	if !strings.Contains(summary, "api_key=[redacted]") {
		t.Fatalf("summary did not redact prefix secret: %q", summary)
	}
	if !strings.Contains(summary, "上游返回 HTML 页面：Upstream Error") {
		t.Fatalf("summary did not include unchanged HTML summary: %q", summary)
	}
}

func TestEmbeddedHTMLSummaryForStatusCases(t *testing.T) {
	longPrefix := "api_key=long-secret " + strings.Repeat("long-prefix ", 40)

	cases := []struct {
		name            string
		input           string
		wantEmpty       bool
		wantContains    []string
		wantNotContains []string
		wantMaxRunes    int
	}{
		{
			name:            "multiple secrets in one prefix",
			input:           "failed api_key=first refresh_token=second token=third <html><title>Denied</title></html>",
			wantContains:    []string{"api_key=[redacted]", "refresh_token=[redacted]", "token=[redacted]", "上游返回 HTML 页面：Denied"},
			wantNotContains: []string{"first", "second", "third"},
		},
		{
			name:            "bearer token prefix",
			input:           "upstream Authorization Bearer abc.def_123+/=- <html><title>Auth Failed</title></html>",
			wantContains:    []string{"Bearer [redacted]", "上游返回 HTML 页面：Auth Failed"},
			wantNotContains: []string{"abc.def_123+/=-"},
		},
		{
			name:            "cookie credential prefix",
			input:           "request cookie=session-secret; <html><title>Cookie Required</title></html>",
			wantContains:    []string{"cookie=[redacted]", "上游返回 HTML 页面：Cookie Required"},
			wantNotContains: []string{"session-secret"},
		},
		{
			name:            "repeated api key prefix",
			input:           "retry api_key=first api_key=second <html><title>Repeated</title></html>",
			wantContains:    []string{"api_key=[redacted] api_key=[redacted]", "上游返回 HTML 页面：Repeated"},
			wantNotContains: []string{"first", "second"},
		},
		{
			name:      "no html returns empty",
			input:     "plain error api_key=secret without markup",
			wantEmpty: true,
		},
		{
			name:      "malformed html marker returns empty",
			input:     "plain error < html api_key=secret",
			wantEmpty: true,
		},
		{
			name:      "empty input returns empty",
			input:     "",
			wantEmpty: true,
		},
		{
			name:            "very long input is truncated by status sanitizer",
			input:           longPrefix + "<html><title>Long Error</title></html>",
			wantContains:    []string{"api_key=[redacted]", "...[truncated]"},
			wantNotContains: []string{"long-secret", "上游返回 HTML 页面：Long Error"},
			wantMaxRunes:    maxSiteStatusMessageRunes,
		},
		{
			name:            "control characters are stripped and whitespace collapsed",
			input:           "prefix\r\twith\x00 api_key=secret\n\n<html><title>Controls</title></html>",
			wantContains:    []string{"prefix with api_key=[redacted]", "上游返回 HTML 页面：Controls"},
			wantNotContains: []string{"secret"},
		},
		{
			name:         "message without secrets keeps html summary",
			input:        "safe prefix <html><title>Safe Error</title></html>",
			wantContains: []string{"safe prefix", "上游返回 HTML 页面：Safe Error"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			summary := embeddedHTMLSummaryForStatus(tc.input)
			if tc.wantMaxRunes > 0 {
				summary = truncateSiteStatusMessage(summary)
			}

			if tc.wantEmpty {
				if summary != "" {
					t.Fatalf("summary = %q, want empty", summary)
				}
				return
			}
			if summary == "" {
				t.Fatal("summary is empty")
			}
			for _, want := range tc.wantContains {
				if !strings.Contains(summary, want) {
					t.Fatalf("summary = %q, want containing %q", summary, want)
				}
			}
			for _, raw := range tc.wantNotContains {
				if strings.Contains(summary, raw) {
					t.Fatalf("summary = %q, leaked %q", summary, raw)
				}
			}
			if containsDisallowedControl(summary) {
				t.Fatalf("summary contains control character: %q", summary)
			}
			if tc.wantMaxRunes > 0 && utf8.RuneCountInString(summary) > tc.wantMaxRunes {
				t.Fatalf("summary length = %d, want <= %d: %q", utf8.RuneCountInString(summary), tc.wantMaxRunes, summary)
			}
		})
	}
}

func containsDisallowedControl(text string) bool {
	for _, r := range text {
		if unicode.IsControl(r) {
			return true
		}
	}
	return false
}
