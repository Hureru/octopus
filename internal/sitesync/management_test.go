package sitesync

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/bestruirui/octopus/internal/model"
)

func TestSyncManagementPlatformDiscoversNewAPIUserID(t *testing.T) {
	observedTokenUserHeader := ""
	observedGroupUserHeader := ""

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		switch {
		case r.URL.Path == "/api/user/self":
			if r.Header.Get("Authorization") != "Bearer test-access-token" {
				w.WriteHeader(http.StatusUnauthorized)
				_, _ = w.Write([]byte(`{"success":false,"message":"unauthorized"}`))
				return
			}
			if r.Header.Get("New-API-User") != "11494" {
				w.WriteHeader(http.StatusUnauthorized)
				_, _ = w.Write([]byte(`{"success":false,"message":"无权进行此操作，未提供 New-Api-User"}`))
				return
			}
			_, _ = w.Write([]byte(`{"success":true,"data":{"id":11494,"username":"managed-user"}}`))
		case r.URL.Path == "/api/token/":
			observedTokenUserHeader = r.Header.Get("New-API-User")
			if r.Header.Get("Authorization") != "Bearer test-access-token" || observedTokenUserHeader != "11494" {
				w.WriteHeader(http.StatusUnauthorized)
				_, _ = w.Write([]byte(`{"success":false,"message":"无权进行此操作，未提供 New-Api-User"}`))
				return
			}
			_, _ = w.Write([]byte(`{"data":{"items":[{"name":"primary","key":"managed-key","group":"vip","status":1}]}}`))
		case r.URL.Path == "/api/user/self/groups":
			observedGroupUserHeader = r.Header.Get("New-API-User")
			if r.Header.Get("Authorization") != "Bearer test-access-token" || observedGroupUserHeader != "11494" {
				w.WriteHeader(http.StatusUnauthorized)
				_, _ = w.Write([]byte(`{"success":false,"message":"无权进行此操作，未提供 New-Api-User"}`))
				return
			}
			_, _ = w.Write([]byte(`{"data":[{"id":"vip","name":"VIP"}]}`))
		case r.URL.Path == "/models":
			if r.Header.Get("Authorization") != "Bearer managed-key" {
				w.WriteHeader(http.StatusUnauthorized)
				_, _ = w.Write([]byte(`{"error":"unauthorized"}`))
				return
			}
			_, _ = w.Write([]byte(`{"data":[{"id":"gpt-4o-mini"}]}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	snapshot, err := syncManagementPlatform(context.Background(), &model.Site{
		Platform: model.SitePlatformNewAPI,
		BaseURL:  server.URL,
	}, &model.SiteAccount{
		Name:           "managed-user",
		CredentialType: model.SiteCredentialTypeAccessToken,
		AccessToken:    "test-access-token",
		Enabled:        true,
		AutoSync:       true,
	})
	if err != nil {
		t.Fatalf("syncManagementPlatform returned error: %v", err)
	}
	if observedTokenUserHeader != "11494" {
		t.Fatalf("expected token sync request to include New-API-User=11494, got %q", observedTokenUserHeader)
	}
	if observedGroupUserHeader != "11494" {
		t.Fatalf("expected group sync request to include New-API-User=11494, got %q", observedGroupUserHeader)
	}
	if len(snapshot.tokens) != 1 || snapshot.tokens[0].Token != "managed-key" {
		t.Fatalf("unexpected synced tokens: %+v", snapshot.tokens)
	}
	if len(snapshot.groups) != 1 || snapshot.groups[0].GroupKey != "vip" {
		t.Fatalf("unexpected synced groups: %+v", snapshot.groups)
	}
	if len(snapshot.models) != 1 || snapshot.models[0].ModelName != "gpt-4o-mini" {
		t.Fatalf("unexpected synced models: %+v", snapshot.models)
	}
}

func TestSyncManagementPlatformUsesStoredNewAPIUserID(t *testing.T) {
	userSelfCalls := 0

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		switch {
		case r.URL.Path == "/api/user/self":
			userSelfCalls++
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"success":false,"message":"should not need probe"}`))
		case r.URL.Path == "/api/token/":
			if r.Header.Get("Authorization") != "Bearer test-access-token" || r.Header.Get("New-API-User") != "7788" {
				w.WriteHeader(http.StatusUnauthorized)
				_, _ = w.Write([]byte(`{"success":false,"message":"无权进行此操作，未提供 New-Api-User"}`))
				return
			}
			_, _ = w.Write([]byte(`{"data":{"items":[{"name":"primary","key":"managed-key","group":"default","status":1}]}}`))
		case r.URL.Path == "/api/user/self/groups":
			if r.Header.Get("Authorization") != "Bearer test-access-token" || r.Header.Get("New-API-User") != "7788" {
				w.WriteHeader(http.StatusUnauthorized)
				_, _ = w.Write([]byte(`{"success":false,"message":"无权进行此操作，未提供 New-Api-User"}`))
				return
			}
			_, _ = w.Write([]byte(`{"data":[{"id":"default","name":"default"}]}`))
		case r.URL.Path == "/models":
			_, _ = w.Write([]byte(`{"data":[{"id":"gpt-4o-mini"}]}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	platformUserID := 7788
	snapshot, err := syncManagementPlatform(context.Background(), &model.Site{
		Platform: model.SitePlatformNewAPI,
		BaseURL:  server.URL,
	}, &model.SiteAccount{
		Name:           "managed-user",
		CredentialType: model.SiteCredentialTypeAccessToken,
		AccessToken:    "test-access-token",
		PlatformUserID: &platformUserID,
		Enabled:        true,
		AutoSync:       true,
	})
	if err != nil {
		t.Fatalf("syncManagementPlatform returned error: %v", err)
	}
	if userSelfCalls != 1 {
		t.Fatalf("expected exactly 1 /api/user/self call (balance fetch), got %d calls", userSelfCalls)
	}
	if len(snapshot.tokens) != 1 || snapshot.tokens[0].Token != "managed-key" {
		t.Fatalf("unexpected synced tokens: %+v", snapshot.tokens)
	}
}

func TestSyncManagementPlatformUsesV1ModelsWhenRootModelEndpointReturnsHTML(t *testing.T) {
	observedV1AuthHeader := ""
	platformUserID := 7788

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/api/token/":
			w.Header().Set("Content-Type", "application/json")
			if r.Header.Get("Authorization") != "Bearer test-access-token" || r.Header.Get("New-API-User") != "7788" {
				w.WriteHeader(http.StatusUnauthorized)
				_, _ = w.Write([]byte(`{"success":false,"message":"无权进行此操作，未提供 New-Api-User"}`))
				return
			}
			_, _ = w.Write([]byte(`{"data":{"items":[{"name":"primary","key":"managed-key","group":"default","status":1}]}}`))
		case r.URL.Path == "/api/user/self/groups":
			w.Header().Set("Content-Type", "application/json")
			if r.Header.Get("Authorization") != "Bearer test-access-token" || r.Header.Get("New-API-User") != "7788" {
				w.WriteHeader(http.StatusUnauthorized)
				_, _ = w.Write([]byte(`{"success":false,"message":"无权进行此操作，未提供 New-Api-User"}`))
				return
			}
			_, _ = w.Write([]byte(`{"data":[{"id":"default","name":"default"}]}`))
		case r.URL.Path == "/models":
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			_, _ = w.Write([]byte(`<!doctype html><html><body>site home</body></html>`))
		case r.URL.Path == "/v1/models":
			w.Header().Set("Content-Type", "application/json")
			observedV1AuthHeader = r.Header.Get("Authorization")
			if observedV1AuthHeader != "Bearer managed-key" {
				w.WriteHeader(http.StatusUnauthorized)
				_, _ = w.Write([]byte(`{"error":"unauthorized"}`))
				return
			}
			_, _ = w.Write([]byte(`{"data":[{"id":"gpt-4o-mini"},{"id":"gpt-4.1"}]}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	snapshot, err := syncManagementPlatform(context.Background(), &model.Site{
		Platform: model.SitePlatformNewAPI,
		BaseURL:  server.URL,
	}, &model.SiteAccount{
		Name:           "managed-user",
		CredentialType: model.SiteCredentialTypeAccessToken,
		AccessToken:    "test-access-token",
		PlatformUserID: &platformUserID,
		Enabled:        true,
		AutoSync:       true,
	})
	if err != nil {
		t.Fatalf("syncManagementPlatform returned error: %v", err)
	}
	if observedV1AuthHeader != "Bearer managed-key" {
		t.Fatalf("expected /v1/models to use managed key, got %q", observedV1AuthHeader)
	}
	if len(snapshot.models) != 2 || snapshot.models[0].ModelName != "gpt-4.1" || snapshot.models[1].ModelName != "gpt-4o-mini" {
		t.Fatalf("unexpected synced models: %+v", snapshot.models)
	}
}

func TestSyncManagementPlatformFallsBackToUserModelsWhenTokenModelsUnavailable(t *testing.T) {
	observedUserModelsHeader := ""
	platformUserID := 7788

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/api/token/":
			w.Header().Set("Content-Type", "application/json")
			if r.Header.Get("Authorization") != "Bearer test-access-token" || r.Header.Get("New-API-User") != "7788" {
				w.WriteHeader(http.StatusUnauthorized)
				_, _ = w.Write([]byte(`{"success":false,"message":"无权进行此操作，未提供 New-Api-User"}`))
				return
			}
			_, _ = w.Write([]byte(`{"data":{"items":[{"name":"primary","key":"managed-key","group":"default","status":1}]}}`))
		case r.URL.Path == "/api/user/self/groups":
			w.Header().Set("Content-Type", "application/json")
			if r.Header.Get("Authorization") != "Bearer test-access-token" || r.Header.Get("New-API-User") != "7788" {
				w.WriteHeader(http.StatusUnauthorized)
				_, _ = w.Write([]byte(`{"success":false,"message":"无权进行此操作，未提供 New-Api-User"}`))
				return
			}
			_, _ = w.Write([]byte(`{"data":[{"id":"default","name":"default"}]}`))
		case r.URL.Path == "/models":
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			_, _ = w.Write([]byte(`<!doctype html><html><body>site home</body></html>`))
		case r.URL.Path == "/v1/models":
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"error":{"message":"unauthorized"}}`))
		case r.URL.Path == "/api/user/models":
			w.Header().Set("Content-Type", "application/json")
			observedUserModelsHeader = r.Header.Get("New-API-User")
			if r.Header.Get("Authorization") != "Bearer test-access-token" || observedUserModelsHeader != "7788" {
				w.WriteHeader(http.StatusUnauthorized)
				_, _ = w.Write([]byte(`{"success":false,"message":"无权进行此操作，未提供 New-Api-User"}`))
				return
			}
			_, _ = w.Write([]byte(`{"data":["gpt-4o-mini","gpt-4.1"]}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	snapshot, err := syncManagementPlatform(context.Background(), &model.Site{
		Platform: model.SitePlatformNewAPI,
		BaseURL:  server.URL,
	}, &model.SiteAccount{
		Name:           "managed-user",
		CredentialType: model.SiteCredentialTypeAccessToken,
		AccessToken:    "test-access-token",
		PlatformUserID: &platformUserID,
		Enabled:        true,
		AutoSync:       true,
	})
	if err != nil {
		t.Fatalf("syncManagementPlatform returned error: %v", err)
	}
	if observedUserModelsHeader != "7788" {
		t.Fatalf("expected /api/user/models to include New-API-User=7788, got %q", observedUserModelsHeader)
	}
	if len(snapshot.models) != 2 || snapshot.models[0].ModelName != "gpt-4.1" || snapshot.models[1].ModelName != "gpt-4o-mini" {
		t.Fatalf("unexpected synced models: %+v", snapshot.models)
	}
}
