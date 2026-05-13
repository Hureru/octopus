package sitesync

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/bestruirui/octopus/internal/model"
)

func TestRequestJSONUsesBrowserHeaders(t *testing.T) {
	observedUserAgent := ""
	observedAccept := ""
	observedAcceptLanguage := ""

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		observedUserAgent = r.Header.Get("User-Agent")
		observedAccept = r.Header.Get("Accept")
		observedAcceptLanguage = r.Header.Get("Accept-Language")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"success":true}`))
	}))
	defer server.Close()

	_, err := requestJSON(context.Background(), &model.Site{BaseURL: server.URL}, http.MethodGet, server.URL, nil, nil)
	if err != nil {
		t.Fatalf("requestJSON returned error: %v", err)
	}
	if !strings.Contains(observedUserAgent, "Mozilla/5.0") {
		t.Fatalf("expected browser user-agent, got %q", observedUserAgent)
	}
	if observedAccept == "" {
		t.Fatalf("expected Accept header to be set")
	}
	if observedAcceptLanguage == "" {
		t.Fatalf("expected Accept-Language header to be set")
	}
}

func TestRequestJSONCustomHeaderOverridesUserAgent(t *testing.T) {
	observedUserAgent := ""
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		observedUserAgent = r.Header.Get("User-Agent")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"success":true}`))
	}))
	defer server.Close()

	_, err := requestJSON(
		context.Background(),
		&model.Site{BaseURL: server.URL, CustomHeader: []model.CustomHeader{{HeaderKey: "User-Agent", HeaderValue: "Octopus-Test-UA"}}},
		http.MethodGet,
		server.URL,
		nil,
		nil,
	)
	if err != nil {
		t.Fatalf("requestJSON returned error: %v", err)
	}
	if observedUserAgent != "Octopus-Test-UA" {
		t.Fatalf("expected custom user-agent, got %q", observedUserAgent)
	}
}

func TestRequestJSONFormatsHTMLErrorSummary(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(http.StatusBadGateway)
		_, _ = w.Write([]byte(`<!DOCTYPE html><html lang="en-US"><head><title>Upstream Error</title></head><body>blocked</body></html>`))
	}))
	defer server.Close()

	_, err := requestJSON(context.Background(), &model.Site{BaseURL: server.URL}, http.MethodGet, server.URL, nil, nil)
	if err == nil {
		t.Fatalf("expected requestJSON to fail")
	}
	if !strings.Contains(err.Error(), "http 502: Upstream Error") {
		t.Fatalf("expected summarized HTML error, got %v", err)
	}
}

func TestRequestJSONDetectsCloudflareAttentionRequired(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("CF-Ray", "abc123-LAX")
		w.Header().Set("Retry-After", "120")
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`<!DOCTYPE html><html><head><title>Attention Required! | Cloudflare</title></head><body>Cloudflare Ray ID: abc123</body></html>`))
	}))
	defer server.Close()

	_, err := requestJSON(context.Background(), &model.Site{BaseURL: server.URL}, http.MethodGet, server.URL, nil, nil)
	if err == nil {
		t.Fatalf("expected requestJSON to fail")
	}
	var cfErr *CloudflareProtectionError
	if !errors.As(err, &cfErr) {
		t.Fatalf("expected CloudflareProtectionError, got %T %v", err, err)
	}
	if cfErr.RetryAfter != 60*time.Second {
		t.Fatalf("expected retry-after capped to 60s, got %s", cfErr.RetryAfter)
	}
}

func TestRequestJSONKeepsJSONForbiddenMessage(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("CF-Ray", "abc123-LAX")
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"message":"token forbidden"}`))
	}))
	defer server.Close()

	_, err := requestJSON(context.Background(), &model.Site{BaseURL: server.URL}, http.MethodGet, server.URL, nil, nil)
	if err == nil {
		t.Fatalf("expected requestJSON to fail")
	}
	if IsCloudflareProtectionError(err) {
		t.Fatalf("expected JSON business error, got Cloudflare error")
	}
	if err.Error() != "http 403: token forbidden" {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestParseGroupItemsPreservesScalarMapLabels(t *testing.T) {
	groups := parseGroupItems(map[string]any{
		"data": map[string]any{
			"vip":   "VIP Group",
			"trial": map[string]any{"name": "Trial Group"},
		},
	})

	seen := make(map[string]string)
	for _, group := range groups {
		seen[group.GroupKey] = group.Name
	}
	if seen["vip"] != "VIP Group" {
		t.Fatalf("expected scalar group label, got %+v", groups)
	}
	if seen["trial"] != "Trial Group" {
		t.Fatalf("expected nested group label, got %+v", groups)
	}
}
