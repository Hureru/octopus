package sitesync

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/bestruirui/octopus/internal/apperror"
	dbpkg "github.com/bestruirui/octopus/internal/db"
	"github.com/bestruirui/octopus/internal/model"
	"github.com/bestruirui/octopus/internal/op"
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
	if got := apperror.Code(err); got != CodeSiteUpstreamCloudflareChallenge {
		t.Fatalf("expected error code %q, got %q", CodeSiteUpstreamCloudflareChallenge, got)
	}
}

func TestRequestJSONDetectsCloudflareAcrossChallengeStatuses(t *testing.T) {
	for _, statusCode := range []int{
		http.StatusOK,
		http.StatusTooManyRequests,
		http.StatusServiceUnavailable,
	} {
		t.Run(http.StatusText(statusCode), func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "text/html; charset=utf-8")
				w.Header().Set("CF-Ray", "challenge-ray")
				w.WriteHeader(statusCode)
				_, _ = w.Write([]byte(`<!doctype html><html><title>Just a moment...</title><body>Cloudflare</body></html>`))
			}))
			defer server.Close()

			_, err := requestJSON(
				context.Background(),
				&model.Site{BaseURL: server.URL},
				http.MethodGet,
				server.URL,
				nil,
				nil,
			)
			if err == nil || !IsCloudflareProtectionError(err) {
				t.Fatalf("status %d Cloudflare challenge was not detected: %v", statusCode, err)
			}
		})
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
	if got := apperror.Code(err); got != CodeSiteUpstreamHTTPError {
		t.Fatalf("expected error code %q, got %q", CodeSiteUpstreamHTTPError, got)
	}
	if got := apperror.Params(err)["statusCode"]; got != http.StatusForbidden {
		t.Fatalf("expected statusCode param %d, got %#v", http.StatusForbidden, got)
	}
}

func TestAnyRouterAppliesHeaderPolicyAndTrustedVerificationSession(t *testing.T) {
	ctx := setupProjectTestDB(t)
	if err := op.InitCache(); err != nil {
		t.Fatalf("initialize op cache: %v", err)
	}
	if err := op.SettingSetString(model.SettingKeyJWTSecret, "anyrouter-verification-test"); err != nil {
		t.Fatalf("set jwt secret: %v", err)
	}

	var observed http.Header
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		observed = r.Header.Clone()
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"success":true}`))
	}))
	defer server.Close()

	siteRecord := model.Site{
		Name:     "anyrouter-header-policy",
		Platform: model.SitePlatformAnyRouter,
		BaseURL:  server.URL,
		Enabled:  true,
	}
	if err := dbpkg.GetDB().WithContext(ctx).Create(&siteRecord).Error; err != nil {
		t.Fatalf("create site: %v", err)
	}
	account := model.SiteAccount{
		SiteID:         siteRecord.ID,
		Name:           "anyrouter-header-account",
		CredentialType: model.SiteCredentialTypeAccessToken,
		AccessToken:    "managed-token",
		Enabled:        true,
	}
	if err := dbpkg.GetDB().WithContext(ctx).Create(&account).Error; err != nil {
		t.Fatalf("create account: %v", err)
	}
	if _, err := op.HeaderPolicyUpsert(ctx, model.HeaderPolicy{
		Scope:   model.HeaderPolicyScopeSite,
		ScopeID: siteRecord.ID,
		Enabled: true,
		SetHeaders: []model.CustomHeader{{
			HeaderKey:   "X-Policy-Applied",
			HeaderValue: "yes",
		}},
	}); err != nil {
		t.Fatalf("create site header policy: %v", err)
	}
	encrypted, err := op.EncryptSecret("cf_clearance=trusted-clearance")
	if err != nil {
		t.Fatalf("encrypt verification cookie: %v", err)
	}
	expiresAt := time.Now().Add(time.Hour)
	account.VerificationCookieEncrypted = encrypted
	account.VerificationUserAgent = "Verified-Browser-UA"
	account.VerificationExpiresAt = &expiresAt

	if _, _, err := anyRouterRequestJSONWithCookies(
		ctx,
		&siteRecord,
		http.MethodGet,
		server.URL,
		nil,
		map[string]string{
			"Cookie":     "session=managed-session; cf_clearance=caller-value",
			"User-Agent": "Caller-UA",
		},
		&account,
	); err != nil {
		t.Fatalf("AnyRouter request failed: %v", err)
	}
	if observed.Get("X-Policy-Applied") != "yes" {
		t.Fatalf("site Header Policy was not applied: %#v", observed)
	}
	if observed.Get("User-Agent") != "Verified-Browser-UA" {
		t.Fatalf("verification User-Agent did not win trusted-last: %#v", observed)
	}
	cookie := observed.Get("Cookie")
	if !strings.Contains(cookie, "session=managed-session") ||
		!strings.Contains(cookie, "cf_clearance=trusted-clearance") ||
		strings.Contains(cookie, "cf_clearance=caller-value") {
		t.Fatalf("trusted verification cookie was not merged safely: %q", cookie)
	}
}

func TestAnyRouterDetectsHTTP200CloudflareChallenge(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("CF-Ray", "anyrouter-challenge")
		_, _ = w.Write([]byte(`<!doctype html><html><title>Just a moment...</title><body>Cloudflare</body></html>`))
	}))
	defer server.Close()

	_, _, err := anyRouterRequestJSONWithCookies(
		context.Background(),
		&model.Site{BaseURL: server.URL},
		http.MethodGet,
		server.URL,
		nil,
		nil,
	)
	if err == nil || !IsCloudflareProtectionError(err) {
		t.Fatalf("AnyRouter 200 challenge was not detected: %v", err)
	}
}

func TestAnyRouterRejectsCloudflareJSONErrorResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("CF-Ray", "anyrouter-json-challenge")
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = w.Write([]byte(`{"message":"Just a moment... Cloudflare challenge"}`))
	}))
	defer server.Close()

	_, _, err := anyRouterRequestJSONWithCookies(
		context.Background(),
		&model.Site{BaseURL: server.URL},
		http.MethodGet,
		server.URL,
		nil,
		nil,
	)
	if err == nil || !IsCloudflareProtectionError(err) {
		t.Fatalf("AnyRouter JSON challenge was accepted as success: %v", err)
	}
}

func TestAnyRouterAcceptsNormalJSONBehindCloudflare(t *testing.T) {
	for _, serverHeader := range []string{"", "cloudflare"} {
		t.Run(serverHeader, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				w.Header().Set("CF-Ray", "normal-api-ray")
				if serverHeader != "" {
					w.Header().Set("Server", serverHeader)
				}
				_, _ = w.Write([]byte(`{"success":true}`))
			}))
			defer server.Close()

			payload, _, err := anyRouterRequestJSONWithCookies(
				context.Background(),
				&model.Site{BaseURL: server.URL},
				http.MethodGet,
				server.URL,
				nil,
				nil,
			)
			if err != nil {
				t.Fatalf("normal Cloudflare-hosted JSON was rejected: %v", err)
			}
			if success, _ := payload["success"].(bool); !success {
				t.Fatalf("unexpected payload: %#v", payload)
			}
		})
	}
}

func TestSiteVerificationHeadersUseExactRecoveryPath(t *testing.T) {
	ctx := setupProjectTestDB(t)
	if err := op.InitCache(); err != nil {
		t.Fatalf("initialize op cache: %v", err)
	}
	if err := op.SettingSetString(model.SettingKeyJWTSecret, "exact-recovery-path-test"); err != nil {
		t.Fatalf("set jwt secret: %v", err)
	}
	encrypted, err := op.EncryptSecret("cf_clearance=bound-node")
	if err != nil {
		t.Fatalf("encrypt verification cookie: %v", err)
	}
	expiresAt := time.Now().Add(time.Hour)
	account := &model.SiteAccount{
		VerificationCookieEncrypted: encrypted,
		VerificationClashNode:       "node-a",
		VerificationExpiresAt:       &expiresAt,
	}
	siteRecord := &model.Site{PreferredClashNode: "node-a"}

	if cookie, _ := siteVerificationHeaders(
		context.Background(),
		siteRecord,
		account,
	); cookie == "" {
		t.Fatal("ordinary preference lookup should match the bound node")
	}

	recoveryCtx := context.WithValue(
		ctx,
		recoveryPathContextKey{},
		siteRecoveryPath{proxyMode: model.ProxyUsageModePool},
	)
	if cookie, _ := siteVerificationHeaders(recoveryCtx, siteRecord, account); cookie != "" {
		t.Fatalf("empty recovery node reused site-preference credentials: cookie=%q", cookie)
	}
}

func TestSiteRecoveryStatusMatchingDoesNotUseNumericSubstrings(t *testing.T) {
	if siteRecoveryErrorRetryable(errors.New("quota 500000 exceeded")) {
		t.Fatal("unrelated numeric text was treated as an HTTP 500")
	}
	if !siteRecoveryErrorRetryable(newSiteHTTPError(http.StatusInternalServerError, "quota exceeded")) {
		t.Fatal("typed HTTP 500 should be retryable")
	}
	if got := classifySiteRecoveryError(errors.New("account 429000 disabled")); got == "rate_limit" {
		t.Fatalf("unrelated numeric text was classified as a rate limit: %s", got)
	}
	if got := classifySiteRecoveryError(newSiteHTTPError(http.StatusTooManyRequests, "slow down")); got != "rate_limit" {
		t.Fatalf("typed HTTP 429 classification = %s, want rate_limit", got)
	}
}

func TestNormalizeModelNamesPreservesCaseDistinctVariants(t *testing.T) {
	models := normalizeModelNames([]string{" GPT-5.5 ", "gpt-5.5", "gpt-5.5", ""})

	if len(models) != 2 {
		t.Fatalf("expected case-distinct model names to be preserved, got %+v", models)
	}
	seen := make(map[string]struct{}, len(models))
	for _, item := range models {
		seen[item] = struct{}{}
	}
	if _, ok := seen["GPT-5.5"]; !ok {
		t.Fatalf("expected GPT-5.5 to be preserved, got %+v", models)
	}
	if _, ok := seen["gpt-5.5"]; !ok {
		t.Fatalf("expected gpt-5.5 to be preserved, got %+v", models)
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
