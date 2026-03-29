package sitesync

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/bestruirui/octopus/internal/model"
)

func TestSyncSub2APIFallsBackToAccessTokenWhenKeyListIsEmpty(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		switch r.URL.Path {
		case "/api/v1/keys":
			if r.Header.Get("Authorization") != "Bearer sub2-session-token" {
				w.WriteHeader(http.StatusUnauthorized)
				_, _ = w.Write([]byte(`{"message":"unauthorized"}`))
				return
			}
			_, _ = w.Write([]byte(`{"data":[]}`))
		case "/api/v1/groups/available":
			if r.Header.Get("Authorization") != "Bearer sub2-session-token" {
				w.WriteHeader(http.StatusUnauthorized)
				_, _ = w.Write([]byte(`{"message":"unauthorized"}`))
				return
			}
			_, _ = w.Write([]byte(`{"data":[{"id":7,"name":"vip"}]}`))
		case "/models":
			if r.Header.Get("Authorization") != "Bearer sub2-session-token" {
				w.WriteHeader(http.StatusUnauthorized)
				_, _ = w.Write([]byte(`{"error":"unauthorized"}`))
				return
			}
			_, _ = w.Write([]byte(`{"data":[{"id":"gpt-4o-mini"},{"id":"claude-3-5-sonnet"}]}`))
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
		t.Fatalf("expected one fallback token, got %+v", snapshot.tokens)
	}
	if snapshot.tokens[0].Token != "sub2-session-token" {
		t.Fatalf("expected fallback token to strip Bearer prefix, got %+v", snapshot.tokens[0])
	}
	if len(snapshot.groups) != 2 {
		t.Fatalf("expected fetched groups plus default fallback group, got %+v", snapshot.groups)
	}
	groupKeys := make(map[string]struct{}, len(snapshot.groups))
	for _, group := range snapshot.groups {
		groupKeys[group.GroupKey] = struct{}{}
	}
	if _, ok := groupKeys["7"]; !ok {
		t.Fatalf("expected groups to include fetched sub2api group 7, got %+v", snapshot.groups)
	}
	if _, ok := groupKeys[model.SiteDefaultGroupKey]; !ok {
		t.Fatalf("expected groups to include default fallback group, got %+v", snapshot.groups)
	}
	if len(snapshot.models) != 2 {
		t.Fatalf("expected models discovered via fallback token, got %+v", snapshot.models)
	}
}
