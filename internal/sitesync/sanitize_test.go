package sitesync

import (
	"strings"
	"testing"
)

func TestEmbeddedHTMLSummaryForStatusSanitizesPrefix(t *testing.T) {
	message := "request failed api_key=secret-value\x00\n<html><title>Upstream Error</title></html>"

	summary := embeddedHTMLSummaryForStatus(message)

	if strings.Contains(summary, "secret-value") {
		t.Fatalf("summary leaked secret: %q", summary)
	}
	if strings.ContainsRune(summary, '\x00') {
		t.Fatalf("summary contains control character: %q", summary)
	}
	if !strings.Contains(summary, "api_key=[redacted]") {
		t.Fatalf("summary did not redact prefix secret: %q", summary)
	}
	if !strings.Contains(summary, "上游返回 HTML 页面：Upstream Error") {
		t.Fatalf("summary did not include unchanged HTML summary: %q", summary)
	}
}
