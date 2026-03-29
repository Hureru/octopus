package sitesync

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/bestruirui/octopus/internal/model"
	"github.com/bestruirui/octopus/internal/op"
)

func TestCreateAccountTokenCreatesManagedKeyAndSyncsAccount(t *testing.T) {
	ctx := setupProjectTestDB(t)

	var createdBody map[string]any

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		switch {
		case r.URL.Path == "/api/user/self":
			if r.Header.Get("Authorization") != "Bearer test-access-token" || r.Header.Get("New-API-User") != "11494" {
				w.WriteHeader(http.StatusUnauthorized)
				_, _ = w.Write([]byte(`{"success":false,"message":"无权进行此操作，未提供 New-Api-User"}`))
				return
			}
			_, _ = w.Write([]byte(`{"success":true,"data":{"id":11494,"username":"managed-user"}}`))
		case r.URL.Path == "/api/token/" && r.Method == http.MethodPost:
			if r.Header.Get("Authorization") != "Bearer test-access-token" || r.Header.Get("New-API-User") != "11494" {
				w.WriteHeader(http.StatusUnauthorized)
				_, _ = w.Write([]byte(`{"success":false,"message":"无权进行此操作，未提供 New-Api-User"}`))
				return
			}
			if err := json.NewDecoder(r.Body).Decode(&createdBody); err != nil {
				t.Fatalf("decode create token body failed: %v", err)
			}
			_, _ = w.Write([]byte(`{"success":true,"data":{"id":1}}`))
		case r.URL.Path == "/api/token/" && r.Method == http.MethodGet:
			if r.Header.Get("Authorization") != "Bearer test-access-token" || r.Header.Get("New-API-User") != "11494" {
				w.WriteHeader(http.StatusUnauthorized)
				_, _ = w.Write([]byte(`{"success":false,"message":"无权进行此操作，未提供 New-Api-User"}`))
				return
			}
			_, _ = w.Write([]byte(`{"data":{"items":[{"name":"vip-created","key":"managed-created-key","group":"vip","status":1}]}}`))
		case r.URL.Path == "/api/user/self/groups":
			if r.Header.Get("Authorization") != "Bearer test-access-token" || r.Header.Get("New-API-User") != "11494" {
				w.WriteHeader(http.StatusUnauthorized)
				_, _ = w.Write([]byte(`{"success":false,"message":"无权进行此操作，未提供 New-Api-User"}`))
				return
			}
			_, _ = w.Write([]byte(`{"data":[{"id":"vip","name":"VIP"}]}`))
		case r.URL.Path == "/models":
			if r.Header.Get("Authorization") != "Bearer managed-created-key" {
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

	site := &model.Site{
		Name:     "managed-create-site",
		Platform: model.SitePlatformNewAPI,
		BaseURL:  server.URL,
		Enabled:  true,
	}
	if err := op.SiteCreate(site, ctx); err != nil {
		t.Fatalf("SiteCreate failed: %v", err)
	}

	account := &model.SiteAccount{
		SiteID:         site.ID,
		Name:           "managed-create-account",
		CredentialType: model.SiteCredentialTypeAccessToken,
		AccessToken:    "test-access-token",
		Enabled:        true,
		AutoSync:       true,
	}
	if err := op.SiteAccountCreate(account, ctx); err != nil {
		t.Fatalf("SiteAccountCreate failed: %v", err)
	}

	result, err := CreateAccountToken(ctx, account.ID, model.SiteChannelKeyCreateRequest{GroupKey: "vip"})
	if err != nil {
		t.Fatalf("CreateAccountToken returned error: %v", err)
	}
	if result == nil || result.TokenCount != 1 {
		t.Fatalf("unexpected sync result: %+v", result)
	}
	if createdBody["group"] != "vip" {
		t.Fatalf("expected created group to be vip, got %#v", createdBody["group"])
	}
	if createdBody["unlimited_quota"] != true {
		t.Fatalf("expected unlimited_quota=true, got %#v", createdBody["unlimited_quota"])
	}
	createdName, _ := createdBody["name"].(string)
	if !strings.HasPrefix(createdName, "octopus-vip-") {
		t.Fatalf("expected generated token name to start with octopus-vip-, got %q", createdName)
	}

	reloaded, err := op.SiteAccountGet(account.ID, ctx)
	if err != nil {
		t.Fatalf("SiteAccountGet failed: %v", err)
	}
	if len(reloaded.Tokens) != 1 || reloaded.Tokens[0].GroupKey != "vip" || reloaded.Tokens[0].Token != "managed-created-key" {
		t.Fatalf("unexpected synced tokens: %+v", reloaded.Tokens)
	}
	if len(reloaded.UserGroups) != 1 || reloaded.UserGroups[0].GroupKey != "vip" {
		t.Fatalf("unexpected synced groups: %+v", reloaded.UserGroups)
	}
	if len(reloaded.Models) != 1 || reloaded.Models[0].GroupKey != "vip" || reloaded.Models[0].ModelName != "gpt-4o-mini" {
		t.Fatalf("unexpected synced models: %+v", reloaded.Models)
	}
}

func TestCreateAccountTokenCreatesSub2APIKeyAndSyncsAccount(t *testing.T) {
	ctx := setupProjectTestDB(t)

	var createdBody map[string]any

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		switch {
		case r.URL.Path == "/api/v1/keys" && r.Method == http.MethodPost:
			if r.Header.Get("Authorization") != "Bearer sub2api-token" {
				w.WriteHeader(http.StatusUnauthorized)
				_, _ = w.Write([]byte(`{"success":false,"message":"unauthorized"}`))
				return
			}
			if err := json.NewDecoder(r.Body).Decode(&createdBody); err != nil {
				t.Fatalf("decode sub2api create body failed: %v", err)
			}
			_, _ = w.Write([]byte(`{"success":true,"data":{"id":31}}`))
		case r.URL.Path == "/api/v1/keys":
			if r.Header.Get("Authorization") != "Bearer sub2api-token" {
				w.WriteHeader(http.StatusUnauthorized)
				_, _ = w.Write([]byte(`{"success":false,"message":"unauthorized"}`))
				return
			}
			_, _ = w.Write([]byte(`{"data":[{"name":"sub2api-created","key":"sub2api-created-key","group_id":"7","group_name":"VIP 7","status":1}]}`))
		case r.URL.Path == "/models":
			if r.Header.Get("Authorization") != "Bearer sub2api-created-key" {
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

	site := &model.Site{
		Name:     "sub2api-create-site",
		Platform: model.SitePlatformSub2API,
		BaseURL:  server.URL,
		Enabled:  true,
	}
	if err := op.SiteCreate(site, ctx); err != nil {
		t.Fatalf("SiteCreate failed: %v", err)
	}

	account := &model.SiteAccount{
		SiteID:         site.ID,
		Name:           "sub2api-create-account",
		CredentialType: model.SiteCredentialTypeAccessToken,
		AccessToken:    "sub2api-token",
		Enabled:        true,
		AutoSync:       true,
	}
	if err := op.SiteAccountCreate(account, ctx); err != nil {
		t.Fatalf("SiteAccountCreate failed: %v", err)
	}

	result, err := CreateAccountToken(context.Background(), account.ID, model.SiteChannelKeyCreateRequest{
		GroupKey: "7",
		Name:     "manual-sub2api-name",
	})
	if err != nil {
		t.Fatalf("CreateAccountToken returned error: %v", err)
	}
	if result == nil || result.TokenCount != 1 {
		t.Fatalf("unexpected sync result: %+v", result)
	}
	if createdBody["group_id"] != float64(7) && createdBody["group_id"] != 7 {
		t.Fatalf("expected group_id=7, got %#v", createdBody["group_id"])
	}
	if createdBody["name"] != "manual-sub2api-name" {
		t.Fatalf("expected provided token name to be used, got %#v", createdBody["name"])
	}

	reloaded, err := op.SiteAccountGet(account.ID, ctx)
	if err != nil {
		t.Fatalf("SiteAccountGet failed: %v", err)
	}
	if len(reloaded.Tokens) != 1 || reloaded.Tokens[0].GroupKey != "7" || reloaded.Tokens[0].Token != "sub2api-created-key" {
		t.Fatalf("unexpected synced tokens: %+v", reloaded.Tokens)
	}
	if len(reloaded.UserGroups) != 1 || reloaded.UserGroups[0].GroupKey != "7" {
		t.Fatalf("unexpected synced groups: %+v", reloaded.UserGroups)
	}
}
