package sitesync

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
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
		case r.URL.Path == "/api/pricing":
			w.Header().Set("Content-Type", "application/json")
			if r.Header.Get("Authorization") != "Bearer test-access-token" || r.Header.Get("New-API-User") != "7788" {
				w.WriteHeader(http.StatusUnauthorized)
				_, _ = w.Write([]byte(`{"success":false,"message":"unauthorized"}`))
				return
			}
			_, _ = w.Write([]byte(`{"data":[
				{"model_name":"gpt-4o-mini","enable_groups":["default"],"supported_endpoint_types":["/v1/chat/completions"]},
				{"model_name":"gpt-4.1","enable_groups":["default"],"supported_endpoint_types":["/v1/chat/completions"]}
			]}`))
		case r.URL.Path == "/api/available_model":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"data":{}}`))
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
	for _, item := range snapshot.models {
		if item.Source != siteModelSourceSyncFallback {
			t.Fatalf("expected fallback models to use source %q, got %+v", siteModelSourceSyncFallback, snapshot.models)
		}
	}
}

func TestSyncManagementPlatformDoesNotFallbackWithoutExplicitGroupMatch(t *testing.T) {
	platformUserID := 7788

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/api/token/":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"data":{"items":[{"name":"primary","key":"managed-key","group":"default","status":1}]}}`))
		case r.URL.Path == "/api/user/self/groups":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"data":[{"id":"default","name":"default"}]}`))
		case r.URL.Path == "/models":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"data":[]}`))
		case r.URL.Path == "/v1/models":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"data":[]}`))
		case r.URL.Path == "/api/user/models":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"data":["gpt-4o-mini"]}`))
		case r.URL.Path == "/api/pricing":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"data":[
				{"model_name":"gpt-4o-mini","supported_endpoint_types":["/v1/chat/completions"]}
			]}`))
		case r.URL.Path == "/api/available_model":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"data":{}}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	_, err := syncManagementPlatform(context.Background(), &model.Site{
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
	if err == nil {
		t.Fatalf("expected syncManagementPlatform to fail when explicit group metadata is missing")
	}
	if !strings.Contains(err.Error(), `站点账号同步失败：所有分组都未能确认模型`) {
		t.Fatalf("expected unresolved-group failure, got %v", err)
	}
}

func TestSyncManagementPlatformPrefersStableGroupErrorOverHTMLSummary(t *testing.T) {
	platformUserID := 7788

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/api/token/":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"data":{"items":[{"name":"primary","key":"managed-key","group":"default","status":1}]}}`))
		case r.URL.Path == "/api/user/self/groups":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"data":[{"id":"default","name":"default"}]}`))
		case r.URL.Path == "/models":
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			_, _ = w.Write([]byte(`<!doctype html><html><head><title>New API</title></head><body>site home</body></html>`))
		case r.URL.Path == "/api/user/models":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"data":["gpt-4o-mini"]}`))
		case r.URL.Path == "/api/pricing":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"data":[{"model_name":"gpt-4o-mini","supported_endpoint_types":["/v1/chat/completions"]}]}`))
		case r.URL.Path == "/api/available_model":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"data":{}}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	_, err := syncManagementPlatform(context.Background(), &model.Site{
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
	if err == nil {
		t.Fatalf("expected syncManagementPlatform to fail when explicit group metadata is missing")
	}
	if strings.Contains(err.Error(), `decode response failed: New API`) {
		t.Fatalf("expected stable group guidance instead of HTML summary, got %v", err)
	}
	if !strings.Contains(err.Error(), `站点账号同步失败：所有分组都未能确认模型`) {
		t.Fatalf("expected unresolved-group failure, got %v", err)
	}
}

func TestSyncManagementPlatformReturnsStableMissingGroupKeyError(t *testing.T) {
	platformUserID := 7788

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		switch {
		case r.URL.Path == "/api/token/":
			_, _ = w.Write([]byte(`{"data":{"items":[]}}`))
		case r.URL.Path == "/api/user/self/groups":
			_, _ = w.Write([]byte(`{"data":[{"id":"vip","name":"VIP"}]}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	_, err := syncManagementPlatform(context.Background(), &model.Site{
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
	if err == nil {
		t.Fatalf("expected syncManagementPlatform to fail when no usable key exists")
	}
	if !strings.Contains(err.Error(), `site sync requires a key for group "default"; create a key for that group on the site and sync again`) {
		t.Fatalf("expected stable missing-key error, got %v", err)
	}
}

func TestSyncManagementPlatformFallsBackUsingAvailableModelExplicitGroups(t *testing.T) {
	platformUserID := 7788

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/api/token/":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"data":{"items":[{"name":"primary","key":"managed-key","group":"default","status":1}]}}`))
		case r.URL.Path == "/api/user/self/groups":
			w.Header().Set("Content-Type", "application/json")
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
			_, _ = w.Write([]byte(`{"data":["gpt-4o-mini","gpt-4.1"]}`))
		case r.URL.Path == "/api/pricing":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"data":[]}`))
		case r.URL.Path == "/api/available_model":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"data":{
				"gpt-4.1":{"enable_groups":["default"],"supported_endpoint_types":["/v1/chat/completions"]}
			}}`))
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
	if len(snapshot.models) != 1 || snapshot.models[0].ModelName != "gpt-4.1" || snapshot.models[0].Source != siteModelSourceSyncFallback {
		t.Fatalf("expected available_model filtered fallback model, got %+v", snapshot.models)
	}
	metadata, ok := model.ParseSiteModelRouteMetadata(snapshot.models[0].RouteRawPayload)
	if !ok || metadata.Source != "/api/available_model" {
		t.Fatalf("expected available_model metadata to be recorded, got %+v", snapshot.models)
	}
}

func TestSyncManagementPlatformMarksAllGroupsEmptyWhenSessionModelsAreEmpty(t *testing.T) {
	platformUserID := 7788

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/api/token/":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"data":{"items":[
				{"name":"default-key","key":"managed-key-default","group":"default","status":1},
				{"name":"vip-key","key":"managed-key-vip","group":"vip","status":1}
			]}}`))
		case r.URL.Path == "/api/user/self/groups":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"data":[{"id":"default","name":"default"},{"id":"vip","name":"VIP"}]}`))
		case r.URL.Path == "/models":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"data":[]}`))
		case r.URL.Path == "/v1/models":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"data":[]}`))
		case r.URL.Path == "/api/user/models":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"data":[]}`))
		case r.URL.Path == "/api/user/self":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"success":true,"data":{"id":7788,"username":"managed-user"}}`))
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
	if snapshot.status != model.SiteExecutionStatusSuccess {
		t.Fatalf("expected success snapshot status, got %q", snapshot.status)
	}
	if len(snapshot.models) != 0 {
		t.Fatalf("expected no models when session model list is empty, got %+v", snapshot.models)
	}
	for _, item := range snapshot.groupResults {
		if item.Status != siteGroupSyncStatusEmpty {
			t.Fatalf("expected all groups to be marked empty, got %+v", snapshot.groupResults)
		}
	}
	if !strings.Contains(snapshot.message, "上游当前无可用模型") {
		t.Fatalf("expected empty-model snapshot message, got %q", snapshot.message)
	}
}

func TestSyncManagementPlatformFallsBackPerFailedGroupWithoutOverwritingExactModels(t *testing.T) {
	platformUserID := 7788
	userModelCalls := 0

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/api/token/":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"data":{"items":[
				{"name":"default-key","key":"managed-key-default","group":"default","status":1},
				{"name":"vip-key","key":"managed-key-vip","group":"vip","status":1}
			]}}`))
		case r.URL.Path == "/api/user/self/groups":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"data":[{"id":"default","name":"default"},{"id":"vip","name":"VIP"}]}`))
		case r.URL.Path == "/models":
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			_, _ = w.Write([]byte(`<!doctype html><html><body>site home</body></html>`))
		case r.URL.Path == "/v1/models":
			w.Header().Set("Content-Type", "application/json")
			switch r.Header.Get("Authorization") {
			case "Bearer managed-key-default":
				_, _ = w.Write([]byte(`{"data":[{"id":"gpt-4o-mini"}]}`))
			case "Bearer managed-key-vip":
				w.WriteHeader(http.StatusUnauthorized)
				_, _ = w.Write([]byte(`{"error":{"message":"unauthorized"}}`))
			default:
				w.WriteHeader(http.StatusUnauthorized)
				_, _ = w.Write([]byte(`{"error":{"message":"unauthorized"}}`))
			}
		case r.URL.Path == "/api/user/models":
			userModelCalls++
			w.Header().Set("Content-Type", "application/json")
			if r.Header.Get("Authorization") != "Bearer test-access-token" || r.Header.Get("New-API-User") != "7788" {
				w.WriteHeader(http.StatusUnauthorized)
				_, _ = w.Write([]byte(`{"success":false,"message":"无权进行此操作，未提供 New-Api-User"}`))
				return
			}
			_, _ = w.Write([]byte(`{"data":["gpt-4.1","gpt-4o"]}`))
		case r.URL.Path == "/api/pricing":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"data":[
				{"model_name":"gpt-4o-mini","enable_groups":["default"],"supported_endpoint_types":["/v1/chat/completions"]},
				{"model_name":"gpt-4.1","enable_groups":["vip"],"supported_endpoint_types":["/v1/chat/completions"]},
				{"model_name":"gpt-4o","enable_groups":["vip"],"supported_endpoint_types":["/v1/chat/completions"]}
			]}`))
		case r.URL.Path == "/api/available_model":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"data":{}}`))
		case r.URL.Path == "/api/user/self":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"success":true,"data":{"id":7788,"username":"managed-user"}}`))
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
	if userModelCalls != 1 {
		t.Fatalf("expected exactly 1 /api/user/models fallback call, got %d", userModelCalls)
	}

	modelsByGroup := make(map[string]map[string]string)
	for _, item := range snapshot.models {
		if _, ok := modelsByGroup[item.GroupKey]; !ok {
			modelsByGroup[item.GroupKey] = make(map[string]string)
		}
		modelsByGroup[item.GroupKey][item.ModelName] = item.Source
	}

	defaultModels := modelsByGroup["default"]
	if len(defaultModels) != 1 || defaultModels["gpt-4o-mini"] != siteModelSourceSync {
		t.Fatalf("expected default group to keep exact models, got %+v", modelsByGroup["default"])
	}

	vipModels := modelsByGroup["vip"]
	if len(vipModels) != 2 || vipModels["gpt-4.1"] != siteModelSourceSyncFallback || vipModels["gpt-4o"] != siteModelSourceSyncFallback {
		t.Fatalf("expected vip group to use fallback models, got %+v", modelsByGroup["vip"])
	}
}

func TestSyncManagementPlatformReturnsPartialWhenSomeGroupsRemainUnresolved(t *testing.T) {
	platformUserID := 7788

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/api/token/":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"data":{"items":[
				{"name":"default-key","key":"managed-key-default","group":"default","status":1},
				{"name":"vip-key","key":"managed-key-vip","group":"vip","status":1}
			]}}`))
		case r.URL.Path == "/api/user/self/groups":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"data":[{"id":"default","name":"default"},{"id":"vip","name":"VIP"}]}`))
		case r.URL.Path == "/models":
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			_, _ = w.Write([]byte(`<!doctype html><html><body>site home</body></html>`))
		case r.URL.Path == "/v1/models":
			w.Header().Set("Content-Type", "application/json")
			switch r.Header.Get("Authorization") {
			case "Bearer managed-key-default":
				_, _ = w.Write([]byte(`{"data":[{"id":"gpt-4o-mini"}]}`))
			case "Bearer managed-key-vip":
				w.WriteHeader(http.StatusUnauthorized)
				_, _ = w.Write([]byte(`{"error":{"message":"unauthorized"}}`))
			default:
				w.WriteHeader(http.StatusUnauthorized)
				_, _ = w.Write([]byte(`{"error":{"message":"unauthorized"}}`))
			}
		case r.URL.Path == "/api/user/models":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"data":["gpt-4o-mini","gpt-4.1"]}`))
		case r.URL.Path == "/api/pricing":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"data":[
				{"model_name":"gpt-4o-mini","enable_groups":["default"],"supported_endpoint_types":["/v1/chat/completions"]},
				{"model_name":"gpt-4.1","supported_endpoint_types":["/v1/chat/completions"]}
			]}`))
		case r.URL.Path == "/api/available_model":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"data":{}}`))
		case r.URL.Path == "/api/user/self":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"success":true,"data":{"id":7788,"username":"managed-user"}}`))
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
	if snapshot.status != model.SiteExecutionStatusPartial {
		t.Fatalf("expected partial snapshot status, got %q", snapshot.status)
	}
	if !strings.Contains(snapshot.message, "部分分组同步完成") {
		t.Fatalf("expected partial snapshot message, got %q", snapshot.message)
	}
	if len(snapshot.models) != 1 || snapshot.models[0].GroupKey != "default" || snapshot.models[0].ModelName != "gpt-4o-mini" {
		t.Fatalf("expected only default group model to be updated, got %+v", snapshot.models)
	}
	groupStatus := make(map[string]siteGroupSyncStatus)
	for _, item := range snapshot.groupResults {
		groupStatus[item.GroupKey] = item.Status
	}
	if groupStatus["default"] != siteGroupSyncStatusSynced {
		t.Fatalf("expected default group synced, got %+v", snapshot.groupResults)
	}
	if groupStatus["vip"] != siteGroupSyncStatusFailed {
		t.Fatalf("expected vip group to be kept as failed, got %+v", snapshot.groupResults)
	}
}

func TestSyncManagementPlatformCachesFallbackUserModelsAcrossFailedGroups(t *testing.T) {
	platformUserID := 7788
	userModelCalls := 0
	pricingCalls := 0
	availableModelCalls := 0

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/api/token/":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"data":{"items":[
				{"name":"default-key","key":"managed-key-default","group":"default","status":1},
				{"name":"vip-key","key":"managed-key-vip","group":"vip","status":1}
			]}}`))
		case r.URL.Path == "/api/user/self/groups":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"data":[{"id":"default","name":"default"},{"id":"vip","name":"VIP"}]}`))
		case r.URL.Path == "/models":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"data":[]}`))
		case r.URL.Path == "/v1/models":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"data":[]}`))
		case r.URL.Path == "/api/user/models":
			userModelCalls++
			w.Header().Set("Content-Type", "application/json")
			if r.Header.Get("Authorization") != "Bearer test-access-token" || r.Header.Get("New-API-User") != "7788" {
				w.WriteHeader(http.StatusUnauthorized)
				_, _ = w.Write([]byte(`{"success":false,"message":"无权进行此操作，未提供 New-Api-User"}`))
				return
			}
			_, _ = w.Write([]byte(`{"data":["gpt-4.1"]}`))
		case r.URL.Path == "/api/pricing":
			pricingCalls++
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"data":[
				{"model_name":"gpt-4.1","enable_groups":["default","vip"],"supported_endpoint_types":["/v1/chat/completions"]}
			]}`))
		case r.URL.Path == "/api/available_model":
			availableModelCalls++
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"data":{}}`))
		case r.URL.Path == "/api/user/self":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"success":true,"data":{"id":7788,"username":"managed-user"}}`))
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
	if userModelCalls != 1 {
		t.Fatalf("expected fallback user models to be fetched once, got %d calls", userModelCalls)
	}
	if pricingCalls != 3 || availableModelCalls != 2 {
		t.Fatalf("expected cached fallback route metadata probes to hit each endpoint twice (managed auth + unauthenticated) plus one pricing sync, got pricing=%d available_model=%d", pricingCalls, availableModelCalls)
	}
	if len(snapshot.models) != 2 {
		t.Fatalf("expected both failed groups to share cached fallback models, got %+v", snapshot.models)
	}
	for _, item := range snapshot.models {
		if item.ModelName != "gpt-4.1" || item.Source != siteModelSourceSyncFallback {
			t.Fatalf("expected cached fallback models for all groups, got %+v", snapshot.models)
		}
	}
}

func TestSyncManagementPlatformAssignsModelsPerTokenGroup(t *testing.T) {
	platformUserID := 7788

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		switch {
		case r.URL.Path == "/api/token/":
			_, _ = w.Write([]byte(`{"data":{"items":[
				{"name":"vip-key","key":"managed-key-vip","group":"vip","status":1},
				{"name":"default-key","key":"managed-key-default","group":"default","status":1}
			]}}`))
		case r.URL.Path == "/api/user/self/groups":
			_, _ = w.Write([]byte(`{"data":[{"id":"default","name":"default"},{"id":"vip","name":"VIP"}]}`))
		case r.URL.Path == "/v1/models":
			switch r.Header.Get("Authorization") {
			case "Bearer managed-key-vip":
				_, _ = w.Write([]byte(`{"data":[{"id":"gpt-4o-mini"}]}`))
			case "Bearer managed-key-default":
				_, _ = w.Write([]byte(`{"data":[{"id":"claude-3-5-sonnet"}]}`))
			default:
				w.WriteHeader(http.StatusUnauthorized)
				_, _ = w.Write([]byte(`{"error":"unauthorized"}`))
			}
		case r.URL.Path == "/api/pricing":
			_, _ = w.Write([]byte(`{"data":[
				{"model_name":"gpt-4o-mini","supported_endpoint_types":["/v1/chat/completions"]},
				{"model_name":"claude-3-5-sonnet","supported_endpoint_types":["/v1/messages"]}
			]}`))
		case r.URL.Path == "/api/user/self":
			_, _ = w.Write([]byte(`{"success":true,"data":{"id":7788,"username":"managed-user"}}`))
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

	if len(snapshot.models) != 2 {
		t.Fatalf("expected exactly 2 grouped models, got %+v", snapshot.models)
	}

	modelsByGroup := make(map[string][]string)
	for _, item := range snapshot.models {
		modelsByGroup[item.GroupKey] = append(modelsByGroup[item.GroupKey], item.ModelName)
	}

	if len(modelsByGroup["vip"]) != 1 || modelsByGroup["vip"][0] != "gpt-4o-mini" {
		t.Fatalf("expected vip group to contain only gpt-4o-mini, got %+v", modelsByGroup["vip"])
	}
	if len(modelsByGroup["default"]) != 1 || modelsByGroup["default"][0] != "claude-3-5-sonnet" {
		t.Fatalf("expected default group to contain only claude-3-5-sonnet, got %+v", modelsByGroup["default"])
	}
}

func TestSyncManagementPlatformAppliesPricingRouteMetadata(t *testing.T) {
	platformUserID := 7788

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		switch {
		case r.URL.Path == "/api/token/":
			_, _ = w.Write([]byte(`{"data":{"items":[{"name":"primary","key":"managed-key","group":"default","status":1}]}}`))
		case r.URL.Path == "/api/user/self/groups":
			_, _ = w.Write([]byte(`{"data":[{"id":"default","name":"default"}]}`))
		case r.URL.Path == "/v1/models":
			_, _ = w.Write([]byte(`{"data":[{"id":"gpt-4o-mini"},{"id":"text-embedding-3-large"},{"id":"vendor-embedding-x"}]}`))
		case r.URL.Path == "/api/pricing":
			if r.Header.Get("Authorization") != "Bearer test-access-token" || r.Header.Get("New-API-User") != "7788" {
				w.WriteHeader(http.StatusUnauthorized)
				_, _ = w.Write([]byte(`{"success":false,"message":"unauthorized"}`))
				return
			}
			_, _ = w.Write([]byte(`{"data":[
				{"model_name":"gpt-4o-mini","supported_endpoint_types":["/v1/responses","/v1/chat/completions"]},
				{"model_name":"text-embedding-3-large","supported_endpoint_types":["/v1/embeddings"]},
				{"model_name":"vendor-embedding-x","supported_endpoint_types":["/vendor/embeddings"]}
			]}`))
		case r.URL.Path == "/api/user/self":
			_, _ = w.Write([]byte(`{"success":true,"data":{"id":7788,"username":"managed-user"}}`))
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

	routeByModel := make(map[string]model.SiteModel)
	for _, item := range snapshot.models {
		routeByModel[item.ModelName] = item
	}

	if routeByModel["gpt-4o-mini"].RouteType != model.SiteModelRouteTypeOpenAIResponse {
		t.Fatalf("expected gpt-4o-mini route type %q, got %q", model.SiteModelRouteTypeOpenAIResponse, routeByModel["gpt-4o-mini"].RouteType)
	}
	if routeByModel["text-embedding-3-large"].RouteType != model.SiteModelRouteTypeOpenAIEmbedding {
		t.Fatalf("expected text-embedding-3-large route type %q, got %q", model.SiteModelRouteTypeOpenAIEmbedding, routeByModel["text-embedding-3-large"].RouteType)
	}
	if routeByModel["vendor-embedding-x"].RouteType != model.SiteModelRouteTypeUnknown {
		t.Fatalf("expected vendor-embedding-x route type %q, got %q", model.SiteModelRouteTypeUnknown, routeByModel["vendor-embedding-x"].RouteType)
	}

	metadata, ok := model.ParseSiteModelRouteMetadata(routeByModel["vendor-embedding-x"].RouteRawPayload)
	if !ok {
		t.Fatalf("expected unsupported model route metadata to be recorded")
	}
	if metadata.RouteSupported {
		t.Fatalf("expected unsupported vendor embedding metadata to remain unsupported")
	}
}

func TestSyncManagementPlatformExpandsModelsToExplicitGroupsWithoutKey(t *testing.T) {
	platformUserID := 7788

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		switch {
		case r.URL.Path == "/api/token/":
			_, _ = w.Write([]byte(`{"data":{"items":[{"name":"primary","key":"managed-key","group":"default","status":1}]}}`))
		case r.URL.Path == "/api/user/self/groups":
			_, _ = w.Write([]byte(`{"data":[{"id":"default","name":"default"},{"id":"vip","name":"VIP"}]}`))
		case r.URL.Path == "/v1/models":
			_, _ = w.Write([]byte(`{"data":[{"id":"gpt-4o-mini"}]}`))
		case r.URL.Path == "/api/pricing":
			_, _ = w.Write([]byte(`{"data":[
				{"model_name":"gpt-4o-mini","enable_groups":["default","vip"],"supported_endpoint_types":["/v1/chat/completions"]}
			]}`))
		case r.URL.Path == "/api/user/self":
			_, _ = w.Write([]byte(`{"success":true,"data":{"id":7788,"username":"managed-user"}}`))
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

	if len(snapshot.models) != 2 {
		t.Fatalf("expected model to be expanded into explicit no-key groups, got %+v", snapshot.models)
	}

	modelsByGroup := make(map[string]model.SiteModel)
	for _, item := range snapshot.models {
		modelsByGroup[item.GroupKey] = item
	}
	if _, ok := modelsByGroup["default"]; !ok {
		t.Fatalf("expected default group model to exist, got %+v", snapshot.models)
	}
	vipModel, ok := modelsByGroup["vip"]
	if !ok {
		t.Fatalf("expected vip group model to be synthesized from explicit groups, got %+v", snapshot.models)
	}
	metadata, ok := model.ParseSiteModelRouteMetadata(vipModel.RouteRawPayload)
	if !ok {
		t.Fatalf("expected synthesized vip model to retain route metadata")
	}
	if len(metadata.EnableGroups) != 2 || metadata.EnableGroups[0] != "default" || metadata.EnableGroups[1] != "vip" {
		t.Fatalf("expected synthesized vip model to keep explicit groups, got %#v", metadata.EnableGroups)
	}
}

func TestSyncManagementPlatformAddsHeuristicResponsesForGPT5(t *testing.T) {
	platformUserID := 7788

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		switch {
		case r.URL.Path == "/api/token/":
			_, _ = w.Write([]byte(`{"data":{"items":[{"name":"primary","key":"managed-key","group":"default","status":1}]}}`))
		case r.URL.Path == "/api/user/self/groups":
			_, _ = w.Write([]byte(`{"data":[{"id":"default","name":"default"}]}`))
		case r.URL.Path == "/v1/models":
			_, _ = w.Write([]byte(`{"data":[{"id":"gpt-5.4"}]}`))
		case r.URL.Path == "/api/pricing":
			_, _ = w.Write([]byte(`{"data":[
				{"model_name":"gpt-5.4","supported_endpoint_types":["/v1/chat/completions"]}
			]}`))
		case r.URL.Path == "/api/user/self":
			_, _ = w.Write([]byte(`{"success":true,"data":{"id":7788,"username":"managed-user"}}`))
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
	if len(snapshot.models) != 1 {
		t.Fatalf("expected one synced model, got %+v", snapshot.models)
	}
	if snapshot.models[0].RouteType != model.SiteModelRouteTypeOpenAIResponse {
		t.Fatalf("expected gpt-5.4 route type %q, got %q", model.SiteModelRouteTypeOpenAIResponse, snapshot.models[0].RouteType)
	}
	metadata, ok := model.ParseSiteModelRouteMetadata(snapshot.models[0].RouteRawPayload)
	if !ok {
		t.Fatalf("expected route metadata to parse")
	}
	if len(metadata.SupportedEndpointTypes) != 1 || metadata.SupportedEndpointTypes[0] != "/v1/chat/completions" {
		t.Fatalf("expected upstream endpoint types to remain chat-only, got %#v", metadata.SupportedEndpointTypes)
	}
	if len(metadata.HeuristicEndpointTypes) != 1 || metadata.HeuristicEndpointTypes[0] != "/v1/responses" {
		t.Fatalf("expected heuristic endpoint types to record injected responses support, got %#v", metadata.HeuristicEndpointTypes)
	}
}
