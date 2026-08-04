package sitesync

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/bestruirui/octopus/internal/model"
)

func TestFetchModelsPrefersActionableFallbackError(t *testing.T) {
	tests := []struct {
		name        string
		status      int
		contentType string
		body        string
		want        SiteBatchReason
	}{
		{name: "cloudflare challenge", status: http.StatusForbidden, contentType: "text/html", body: "<!doctype html><title>Attention Required! | Cloudflare</title>", want: SiteBatchReasonCloudflareProtection},
		{name: "invalid credential", status: http.StatusUnauthorized, contentType: "application/json", body: `{"error":{"message":"invalid fixture credential"}}`, want: SiteBatchReasonUnauthorized},
		{name: "upstream failure", status: http.StatusInternalServerError, contentType: "application/json", body: `{"error":{"message":"fixture upstream failure"}}`, want: SiteBatchReasonUpstreamHTTPError},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", tt.contentType)
				if r.URL.Path == "/models" {
					w.WriteHeader(http.StatusNotFound)
					_, _ = w.Write([]byte(`{"error":{"message":"probe route not found"}}`))
					return
				}
				w.WriteHeader(tt.status)
				_, _ = w.Write([]byte(tt.body))
			}))
			defer server.Close()

			_, err := fetchModelsForSiteToken(
				context.Background(),
				&model.Site{BaseURL: server.URL, Platform: model.SitePlatformAPI},
				&model.SiteAccount{},
				model.SiteToken{Token: "fixture-key"},
			)
			if err == nil {
				t.Fatal("fetchModelsForSiteToken() error = nil")
			}
			if got := classifySiteBatchMessage(err.Error()); got != tt.want {
				t.Fatalf("failure reason = %q, want %q (error: %v)", got, tt.want, err)
			}
		})
	}
}

func TestFetchModelsOneHubPrefersActionableAvailableModelError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/available_model" {
			w.Header().Set("Content-Type", "text/html")
			w.Header().Set("CF-Ray", "fixture-LAX")
			w.WriteHeader(http.StatusForbidden)
			_, _ = w.Write([]byte("<!doctype html><title>Attention Required! | Cloudflare</title>"))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"error":{"message":"probe route not found"}}`))
	}))
	defer server.Close()

	_, err := fetchModelsForSiteToken(
		context.Background(),
		&model.Site{BaseURL: server.URL, Platform: model.SitePlatformOneHub},
		&model.SiteAccount{},
		model.SiteToken{Token: "fixture-key"},
	)
	if err == nil {
		t.Fatal("fetchModelsForSiteToken() error = nil")
	}
	if got := classifySiteBatchMessage(err.Error()); got != SiteBatchReasonCloudflareProtection {
		t.Fatalf("failure reason = %q, want %q (error: %v)", got, SiteBatchReasonCloudflareProtection, err)
	}
}

func TestFetchModelsOneHubUsesAvailableModelResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/api/available_model" {
			_, _ = w.Write([]byte(`{"data":{"claude-sonnet-4-5":{},"gpt-4.1-mini":{}}}`))
			return
		}
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"error":{"message":"probe route not found"}}`))
	}))
	defer server.Close()

	models, err := fetchModelsForSiteToken(
		context.Background(),
		&model.Site{BaseURL: server.URL, Platform: model.SitePlatformOneHub},
		&model.SiteAccount{},
		model.SiteToken{Token: "fixture-key"},
	)
	if err != nil {
		t.Fatalf("fetchModelsForSiteToken() error = %v", err)
	}
	want := []string{"claude-sonnet-4-5", "gpt-4.1-mini"}
	if len(models) != len(want) {
		t.Fatalf("models = %v, want %v", models, want)
	}
	for index := range want {
		if models[index] != want[index] {
			t.Fatalf("models = %v, want %v", models, want)
		}
	}
}
