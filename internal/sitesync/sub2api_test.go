package sitesync

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/bestruirui/octopus/internal/model"
)

func TestSyncSub2APIUsesManagedKeyAndAPIModelEndpoint(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		switch r.URL.Path {
		case "/api/v1/keys":
			if r.Header.Get("Authorization") != "Bearer sub2-session-token" {
				w.WriteHeader(http.StatusUnauthorized)
				_, _ = w.Write([]byte(`{"message":"unauthorized"}`))
				return
			}
			_, _ = w.Write([]byte(`{"code":0,"data":{"items":[{"id":11,"name":"managed-key","key":"sub2-user-key","group_id":7,"group_name":"VIP 7","enabled":true}]}}`))
		case "/api/v1/groups/available":
			if r.Header.Get("Authorization") != "Bearer sub2-session-token" {
				w.WriteHeader(http.StatusUnauthorized)
				_, _ = w.Write([]byte(`{"message":"unauthorized"}`))
				return
			}
			_, _ = w.Write([]byte(`{"code":0,"data":{"groups":[{"id":7,"name":"vip"}]}}`))
		case "/v1/models":
			http.NotFound(w, r)
		case "/api/v1/models":
			if r.Header.Get("Authorization") != "Bearer sub2-user-key" {
				w.WriteHeader(http.StatusUnauthorized)
				_, _ = w.Write([]byte(`{"error":"unauthorized"}`))
				return
			}
			_, _ = w.Write([]byte(`{"code":0,"data":{"items":[{"id":"gpt-4o-mini"},{"name":"claude-3-5-sonnet"}]}}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	snapshot, err := syncSub2API(context.Background(), &model.Site{
		BaseURL:  server.URL,
		Platform: model.SitePlatformSub2API,
	}, &model.SiteAccount{
		CredentialType: model.SiteCredentialTypeAccessToken,
		AccessToken:    "Bearer sub2-session-token",
	})
	if err != nil {
		t.Fatalf("syncSub2API returned error: %v", err)
	}
	if len(snapshot.tokens) != 1 {
		t.Fatalf("expected one managed token, got %+v", snapshot.tokens)
	}
	if snapshot.tokens[0].Token != "sub2-user-key" || snapshot.tokens[0].GroupKey != "7" {
		t.Fatalf("expected managed token with group 7, got %+v", snapshot.tokens[0])
	}
	if len(snapshot.groups) != 1 || snapshot.groups[0].GroupKey != "7" || snapshot.groups[0].Name != "vip" {
		t.Fatalf("expected parsed group 7/vip, got %+v", snapshot.groups)
	}
	if len(snapshot.models) != 2 {
		t.Fatalf("expected models discovered from /api/v1/models, got %+v", snapshot.models)
	}
}

func TestSyncSub2APIRequiresRealAPIKeyWhenKeyListIsEmpty(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		switch r.URL.Path {
		case "/api/v1/keys", "/api/v1/api-keys":
			_, _ = w.Write([]byte(`{"code":0,"data":[]}`))
		case "/api/v1/groups/available", "/api/v1/groups", "/api/v1/group":
			_, _ = w.Write([]byte(`{"code":0,"data":[{"id":7,"name":"vip"}]}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	_, err := syncSub2API(context.Background(), &model.Site{
		BaseURL:  server.URL,
		Platform: model.SitePlatformSub2API,
	}, &model.SiteAccount{
		CredentialType: model.SiteCredentialTypeAccessToken,
		AccessToken:    "sub2-session-token",
	})
	if err == nil {
		t.Fatalf("expected syncSub2API to require an API key")
	}
	if !strings.Contains(strings.ToLower(err.Error()), "api key") {
		t.Fatalf("expected API key error, got %v", err)
	}
}

func TestFetchSub2APITokensReturnsEnvelopeError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"code":401,"message":"token expired","data":null}`))
	}))
	defer server.Close()

	_, err := fetchSub2APITokens(context.Background(), &model.Site{BaseURL: server.URL}, &model.SiteAccount{}, "expired-token")
	if err == nil {
		t.Fatalf("expected envelope error")
	}
	if !strings.Contains(strings.ToLower(err.Error()), "expired") {
		t.Fatalf("expected token expired error, got %v", err)
	}
}

func TestBuildSub2APIModelEndpointURLsIncludesAntigravityV1(t *testing.T) {
	endpoints := buildSub2APIModelEndpointURLs(&model.Site{BaseURL: "https://example.com"})
	for _, endpoint := range endpoints {
		if endpoint == "https://example.com/antigravity/v1/models" {
			return
		}
	}
	t.Fatalf("expected antigravity v1 models endpoint, got %+v", endpoints)
}

func TestParseSub2APIModelNamesReturnsEnvelopeError(t *testing.T) {
	_, err := parseSub2APIModelNames(map[string]any{
		"code":    float64(401),
		"message": "expired key",
	})
	if err == nil {
		t.Fatalf("expected envelope error")
	}
	if !strings.Contains(strings.ToLower(err.Error()), "expired") {
		t.Fatalf("expected expired key error, got %v", err)
	}
}
