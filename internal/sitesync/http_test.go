package sitesync

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

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

func TestRequestJSONFormatsHTMLErrorSummary(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`<!DOCTYPE html><html lang="en-US"><head><title>Just a moment...</title></head><body>blocked</body></html>`))
	}))
	defer server.Close()

	_, err := requestJSON(context.Background(), &model.Site{BaseURL: server.URL}, http.MethodGet, server.URL, nil, nil)
	if err == nil {
		t.Fatalf("expected requestJSON to fail")
	}
	if !strings.Contains(err.Error(), "http 403: Just a moment...") {
		t.Fatalf("expected summarized HTML error, got %v", err)
	}
}
