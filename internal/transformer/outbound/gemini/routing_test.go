package gemini

import (
	"context"
	"io"
	"net/url"
	"testing"

	"github.com/samber/lo"

	"github.com/bestruirui/octopus/internal/transformer/model"
)

func newGeminiRequestForRouting(stream bool) *model.InternalLLMRequest {
	req := &model.InternalLLMRequest{
		Model: "gemini-2.5-pro",
		Messages: []model.Message{
			{Role: "user", Content: model.MessageContent{Content: stringPtr("hi")}},
		},
	}
	if stream {
		req.Stream = lo.ToPtr(true)
	}
	return req
}

// G-H5: a base URL without an API-version segment should auto-prepend
// `/v1beta`; otherwise Gemini returns 404.
func TestTransformRequestFillsDefaultGeminiApiVersion(t *testing.T) {
	outbound := &MessagesOutbound{}
	req, err := outbound.TransformRequest(
		context.Background(),
		newGeminiRequestForRouting(false),
		"https://generativelanguage.googleapis.com",
		"secret-key",
	)
	if err != nil {
		t.Fatalf("TransformRequest: %v", err)
	}
	_, _ = io.Copy(io.Discard, req.Body)

	u, err := url.Parse(req.URL.String())
	if err != nil {
		t.Fatalf("parse url: %v", err)
	}
	if u.Path != "/v1beta/models/gemini-2.5-pro:generateContent" {
		t.Fatalf("expected /v1beta prepended, got %s", u.Path)
	}
}

// Explicit `/v1` or `/v1beta` paths must survive verbatim — we only back-fill
// when no version is present at all.
func TestTransformRequestPreservesExplicitGeminiApiVersion(t *testing.T) {
	cases := []struct {
		name       string
		baseURL    string
		wantPrefix string
	}{
		{"v1", "https://generativelanguage.googleapis.com/v1", "/v1"},
		{"v1beta", "https://generativelanguage.googleapis.com/v1beta/", "/v1beta"},
		{"v1alpha", "https://generativelanguage.googleapis.com/v1alpha", "/v1alpha"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			outbound := &MessagesOutbound{}
			req, err := outbound.TransformRequest(
				context.Background(),
				newGeminiRequestForRouting(false),
				c.baseURL,
				"secret-key",
			)
			if err != nil {
				t.Fatalf("TransformRequest: %v", err)
			}
			_, _ = io.Copy(io.Discard, req.Body)
			u := req.URL
			wantPath := c.wantPrefix + "/models/gemini-2.5-pro:generateContent"
			if u.Path != wantPath {
				t.Fatalf("expected path %s, got %s", wantPath, u.Path)
			}
		})
	}
}

// G-H6: the API key must ride in `x-goog-api-key` instead of the query
// string, so it never leaks into proxy logs.
func TestTransformRequestSendsApiKeyAsHeader(t *testing.T) {
	outbound := &MessagesOutbound{}
	req, err := outbound.TransformRequest(
		context.Background(),
		newGeminiRequestForRouting(false),
		"https://generativelanguage.googleapis.com/v1beta",
		"secret-key",
	)
	if err != nil {
		t.Fatalf("TransformRequest: %v", err)
	}
	_, _ = io.Copy(io.Discard, req.Body)

	if got := req.Header.Get("x-goog-api-key"); got != "secret-key" {
		t.Fatalf("expected x-goog-api-key header, got %q", got)
	}
	if req.URL.Query().Get("key") != "" {
		t.Fatalf("expected no key= query param, got URL %s", req.URL.String())
	}
}

// Streaming requests still need `alt=sse` on the query string; only the
// `key` parameter should move to a header.
func TestTransformRequestStreamKeepsAltSse(t *testing.T) {
	outbound := &MessagesOutbound{}
	req, err := outbound.TransformRequest(
		context.Background(),
		newGeminiRequestForRouting(true),
		"https://generativelanguage.googleapis.com/v1beta",
		"secret-key",
	)
	if err != nil {
		t.Fatalf("TransformRequest: %v", err)
	}
	_, _ = io.Copy(io.Discard, req.Body)

	if req.URL.Query().Get("alt") != "sse" {
		t.Fatalf("expected alt=sse for streaming, got %s", req.URL.String())
	}
	if req.URL.Query().Get("key") != "" {
		t.Fatalf("expected api key to be moved off the query, got %s", req.URL.String())
	}
	if got := req.Header.Get("x-goog-api-key"); got != "secret-key" {
		t.Fatalf("expected header carried api key, got %q", got)
	}
	if got := req.URL.Path; got != "/v1beta/models/gemini-2.5-pro:streamGenerateContent" {
		t.Fatalf("expected stream method path, got %s", got)
	}
}

// pathHasGeminiVersion isolates the small heuristic so future docs-only
// additions (e.g. v2) are easy to spot in tests.
func TestPathHasGeminiVersion(t *testing.T) {
	cases := []struct {
		path string
		want bool
	}{
		{"", false},
		{"/", false},
		{"/v1", true},
		{"/v1beta", true},
		{"/v1beta/", true},
		{"/v1alpha/models", true},
		{"/viewer", false},
		{"/proxy/v1beta", false}, // only matches first segment
	}
	for _, c := range cases {
		if got := pathHasGeminiVersion(c.path); got != c.want {
			t.Errorf("pathHasGeminiVersion(%q) = %v, want %v", c.path, got, c.want)
		}
	}
}
