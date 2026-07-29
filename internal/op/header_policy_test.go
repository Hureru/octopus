package op

import (
	"encoding/json"
	"errors"
	"net/http"
	"slices"
	"strings"
	"testing"

	dbpkg "github.com/bestruirui/octopus/internal/db"
	"github.com/bestruirui/octopus/internal/model"
	"gorm.io/gorm"
)

func TestResolveHeaderPolicyUsesSafeDefaultClientAllowList(t *testing.T) {
	ctx := setupBackupTestDB(t)
	policy, err := ResolveHeaderPolicy(ctx, 0, 0, 0)
	if err != nil {
		t.Fatalf("ResolveHeaderPolicy failed: %v", err)
	}
	if !policy.ForwardClientHeaders || len(policy.AllowedClientHeaders) == 0 {
		t.Fatalf("expected safe default forwarding policy, got %+v", policy)
	}

	outbound := http.Header{"Authorization": []string{"Bearer upstream"}}
	client := http.Header{
		"Accept-Language":      []string{"zh-CN"},
		"User-Agent":           []string{"client-agent"},
		"X-Stainless-Runtime":  []string{"go"},
		"X-Custom-Secret-Hint": []string{"must-not-forward"},
		"Authorization":        []string{"Bearer client"},
		"X-Forwarded-For":      []string{"198.51.100.1"},
	}
	ApplyHeaderPolicy(outbound, client, nil, policy)

	if outbound.Get("Accept-Language") != "zh-CN" ||
		outbound.Get("User-Agent") != "client-agent" ||
		outbound.Get("X-Stainless-Runtime") != "go" {
		t.Fatalf("safe headers were not forwarded: %#v", outbound)
	}
	if outbound.Get("X-Custom-Secret-Hint") != "" || outbound.Get("X-Forwarded-For") != "" {
		t.Fatalf("unsafe client headers were forwarded: %#v", outbound)
	}
	if outbound.Get("Authorization") != "Bearer upstream" {
		t.Fatalf("client authorization overrode upstream credentials: %#v", outbound)
	}
}

func TestHeaderPolicyUpsertVersionsNamedPoliciesAndTracesMetadata(t *testing.T) {
	ctx := setupBackupTestDB(t)
	created, err := HeaderPolicyUpsert(ctx, model.HeaderPolicy{
		Name:    "Codex compatibility",
		Scope:   model.HeaderPolicyScopeGlobal,
		Enabled: true,
		SetHeaders: []model.CustomHeader{{
			HeaderKey:   "X-Policy-Marker",
			HeaderValue: "must-not-appear-in-trace",
		}},
	})
	if err != nil {
		t.Fatalf("create header policy: %v", err)
	}
	if created.Name != "Codex compatibility" || created.Version != 1 {
		t.Fatalf("new policy metadata = %+v, want name and version 1", created)
	}

	created.Name = ""
	created.UserAgent = stringPointer("codex_cli_rs/1.0")
	updated, err := HeaderPolicyUpsert(ctx, *created)
	if err != nil {
		t.Fatalf("update header policy: %v", err)
	}
	if updated.Name != "Codex compatibility" || updated.Version != 2 {
		t.Fatalf("legacy empty-name update did not preserve metadata: %+v", updated)
	}

	resolved, err := ResolveHeaderPolicy(ctx, 0, 0, 0)
	if err != nil {
		t.Fatalf("resolve header policy: %v", err)
	}
	if len(resolved.Trace) != 1 {
		t.Fatalf("resolved trace = %+v, want one policy", resolved.Trace)
	}
	trace := resolved.Trace[0]
	if trace.PolicyID != updated.ID ||
		trace.PolicyName != updated.Name ||
		trace.PolicyVersion != updated.Version ||
		!slices.Contains(trace.AppliedKeys, "X-Policy-Marker") {
		t.Fatalf("trace metadata is incomplete: %+v", trace)
	}
	payload, err := json.Marshal(resolved.Trace)
	if err != nil {
		t.Fatalf("marshal trace: %v", err)
	}
	if strings.Contains(string(payload), "must-not-appear-in-trace") ||
		strings.Contains(string(payload), "codex_cli_rs/1.0") {
		t.Fatalf("trace leaked a header value: %s", payload)
	}
}

func TestHeaderPolicyResolverFailureUsesProtectedFailClosedFallback(t *testing.T) {
	ctx := setupBackupTestDB(t)
	callbackName := "test:fail-header-policy-query"
	if err := dbpkg.GetDB().Callback().Query().Before("gorm:query").
		Register(callbackName, func(tx *gorm.DB) {
			if tx.Statement.Schema != nil && tx.Statement.Schema.Name == "HeaderPolicy" {
				tx.AddError(errors.New("policy database unavailable"))
			}
		}); err != nil {
		t.Fatalf("register query callback: %v", err)
	}
	t.Cleanup(func() {
		_ = dbpkg.GetDB().Callback().Query().Remove(callbackName)
	})

	policy, err := ResolveHeaderPolicy(ctx, 0, 0, 0)
	if err == nil {
		t.Fatal("resolver failure was hidden")
	}
	if policy.ForwardClientHeaders ||
		policy.AllowedClientHeaders == nil ||
		len(policy.AllowedClientHeaders) != 0 {
		t.Fatalf("resolver error did not fail closed: %+v", policy)
	}
	sitePolicy, err := ResolveSiteHeaderPolicy(ctx, 1, 0)
	if err == nil || sitePolicy.ForwardClientHeaders {
		t.Fatalf("site resolver error did not fail closed: policy=%+v err=%v", sitePolicy, err)
	}

	outbound := http.Header{}
	ApplyHeaderPolicy(
		outbound,
		http.Header{"Accept": []string{"application/json"}},
		[]model.CustomHeader{
			{HeaderKey: "X-Legacy-Safe", HeaderValue: "kept"},
			{HeaderKey: "Cookie", HeaderValue: "blocked"},
		},
		policy,
	)
	if outbound.Get("Accept") != "" ||
		outbound.Get("Cookie") != "" ||
		outbound.Get("X-Legacy-Safe") != "kept" {
		t.Fatalf("fail-closed fallback lost compatibility or protection: %#v", outbound)
	}
}

func TestHeaderPolicyRegistryMatchesRuntimeProtection(t *testing.T) {
	registry := HeaderPolicyRegistry()
	if len(registry.DefaultAllowedClientHeaders) == 0 ||
		len(registry.ProtectedHeaders) == 0 ||
		len(registry.ProtectedPrefixes) == 0 {
		t.Fatalf("registry should expose all policy boundaries: %+v", registry)
	}
	for _, header := range registry.ProtectedHeaders {
		if !HeaderIsProtected(header) {
			t.Fatalf("registry header %q is not protected at runtime", header)
		}
	}
	for _, prefix := range registry.ProtectedPrefixes {
		if !HeaderIsProtected(prefix + "test") {
			t.Fatalf("registry prefix %q is not protected at runtime", prefix)
		}
	}
}

func TestApplyHeaderPolicyExplicitEmptyListDeniesAllClientHeaders(t *testing.T) {
	outbound := http.Header{}
	client := http.Header{
		"Accept":     []string{"application/json"},
		"User-Agent": []string{"client-agent"},
	}
	ApplyHeaderPolicy(outbound, client, nil, model.ResolvedHeaderPolicy{
		ForwardClientHeaders: true,
		AllowedClientHeaders: []string{},
	})
	if len(outbound) != 0 {
		t.Fatalf("explicit empty allow-list should deny all client headers: %#v", outbound)
	}
}

func TestResolveHeaderPolicyPreservesExplicitEmptyAllowList(t *testing.T) {
	ctx := setupBackupTestDB(t)
	if _, err := HeaderPolicyUpsert(ctx, model.HeaderPolicy{
		Scope:                model.HeaderPolicyScopeGlobal,
		Enabled:              true,
		AllowedClientHeaders: []string{},
	}); err != nil {
		t.Fatalf("HeaderPolicyUpsert failed: %v", err)
	}

	policy, err := ResolveHeaderPolicy(ctx, 0, 0, 0)
	if err != nil {
		t.Fatalf("ResolveHeaderPolicy failed: %v", err)
	}
	if policy.AllowedClientHeaders == nil || len(policy.AllowedClientHeaders) != 0 {
		t.Fatalf("explicit empty allow-list must remain [], got %#v", policy.AllowedClientHeaders)
	}
}

func TestApplyHeaderPolicySupportsPrefixRulesAndBetaMultivalue(t *testing.T) {
	outbound := http.Header{"Anthropic-Beta": []string{"prompt-caching-2024-07-31"}}
	client := http.Header{
		"X-Trace-One":    []string{"one"},
		"X-Trace-Two":    []string{"two"},
		"X-Other":        []string{"blocked"},
		"X-Forwarded-Id": []string{"protected"},
		"Anthropic-Beta": []string{
			"extended-cache-ttl-2025-04-11",
			"prompt-caching-2024-07-31",
		},
	}
	ApplyHeaderPolicy(outbound, client, nil, model.ResolvedHeaderPolicy{
		ForwardClientHeaders: true,
		AllowedClientHeaders: []string{"x-trace-*", "anthropic-beta", "*"},
	})

	if outbound.Get("X-Trace-One") != "one" || outbound.Get("X-Trace-Two") != "two" {
		t.Fatalf("prefix-allowed headers missing: %#v", outbound)
	}
	if outbound.Get("X-Forwarded-Id") != "" {
		t.Fatalf("protected prefix bypassed policy: %#v", outbound)
	}
	betas := splitCommaHeader(outbound.Get("Anthropic-Beta"))
	if !slices.Equal(betas, []string{
		"prompt-caching-2024-07-31",
		"extended-cache-ttl-2025-04-11",
	}) {
		t.Fatalf("anthropic beta values were not merged deterministically: %#v", betas)
	}
}

func TestHeaderPolicyRejectsInvalidNamesValuesAndProtectedHeaders(t *testing.T) {
	ctx := setupBackupTestDB(t)
	tests := []model.HeaderPolicy{
		{
			Scope:      model.HeaderPolicyScopeGlobal,
			Enabled:    true,
			SetHeaders: []model.CustomHeader{{HeaderKey: "X-Test", HeaderValue: "ok\r\nInjected: yes"}},
		},
		{
			Scope:     model.HeaderPolicyScopeGlobal,
			Enabled:   true,
			UserAgent: stringPointer("agent\nInjected: yes"),
		},
		{
			Scope:      model.HeaderPolicyScopeGlobal,
			Enabled:    true,
			SetHeaders: []model.CustomHeader{{HeaderKey: "Authorization", HeaderValue: "Bearer bad"}},
		},
		{
			Scope:                model.HeaderPolicyScopeGlobal,
			Enabled:              true,
			AllowedClientHeaders: []string{"Bad Header"},
		},
	}
	for index := range tests {
		if _, err := HeaderPolicyUpsert(ctx, tests[index]); err == nil {
			t.Fatalf("invalid policy %d was accepted: %+v", index, tests[index])
		}
	}
}

func TestApplyHeaderPolicyDropsInvalidLegacyValues(t *testing.T) {
	outbound := http.Header{}
	ApplyHeaderPolicy(outbound, nil, []model.CustomHeader{
		{HeaderKey: "X-Good", HeaderValue: "ok"},
		{HeaderKey: "X-Bad", HeaderValue: "ok\r\nInjected: yes"},
		{HeaderKey: "Authorization", HeaderValue: "Bearer bad"},
	}, model.ResolvedHeaderPolicy{})
	if outbound.Get("X-Good") != "ok" {
		t.Fatalf("valid legacy header missing: %#v", outbound)
	}
	if outbound.Get("X-Bad") != "" || outbound.Get("Authorization") != "" {
		t.Fatalf("unsafe legacy header was applied: %#v", outbound)
	}
}

func splitCommaHeader(value string) []string {
	result := make([]string, 0)
	for _, part := range strings.Split(value, ",") {
		if trimmed := strings.TrimSpace(part); trimmed != "" {
			result = append(result, trimmed)
		}
	}
	return result
}

func stringPointer(value string) *string {
	return &value
}
