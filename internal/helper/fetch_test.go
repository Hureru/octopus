package helper

import (
	"context"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/bestruirui/octopus/internal/model"
	"github.com/bestruirui/octopus/internal/transformer/outbound"
)

func TestFetchModelsProbesEndpoints(t *testing.T) {
	const list, empty = `{"data":[{"id":"m1"}]}`, `{"data":[]}`
	tests := []struct {
		name     string
		typ      outbound.OutboundType
		basePath string
		upstream map[string]string // "<auth header> <path>" -> 200 body; "drop" hangs up; anything else is a 404
		want     []string          // nil: an error is expected
		requests int32
	}{
		{"site root falls back to /v1/models", outbound.OutboundTypeOpenAIChat, "",
			map[string]string{"Authorization /v1/models": list}, []string{"m1"}, 2},
		{"versioned base URL is not probed", outbound.OutboundTypeOpenAIChat, "/v1",
			nil, nil, 1},
		{"anthropic falls back to the OpenAI-compatible listing", outbound.OutboundTypeAnthropic, "/v1",
			map[string]string{"Authorization /v1/models": list}, []string{"m1"}, 2},
		{"gemini site root reaches /v1beta/models", outbound.OutboundTypeGemini, "",
			map[string]string{"X-Goog-Api-Key /v1beta/models": `{"models":[{"name":"models/m1"}]}`}, []string{"m1"}, 4},
		{"empty guess does not mask the configured endpoint's error", outbound.OutboundTypeOpenAIChat, "",
			map[string]string{"Authorization /v1/models": empty}, nil, 4},
		{"empty fallback does not mask the native error", outbound.OutboundTypeAnthropic, "/v1",
			map[string]string{"Authorization /v1/models": empty}, nil, 2},
		{"empty native listing still yields the fallback's error", outbound.OutboundTypeAnthropic, "/v1",
			map[string]string{"X-Api-Key /v1/models": empty}, nil, 2},
		{"unreachable upstream is tried once", outbound.OutboundTypeAnthropic, "",
			map[string]string{"X-Api-Key /models": "drop"}, nil, 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var requests atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requests.Add(1)
				auth := "Authorization"
				for _, header := range []string{"X-Api-Key", "X-Goog-Api-Key"} {
					if r.Header.Get(header) != "" {
						auth = header
					}
				}
				body, ok := tt.upstream[auth+" "+r.URL.Path]
				switch {
				case !ok:
					http.NotFound(w, r)
				case body == "drop":
					conn, _, _ := w.(http.Hijacker).Hijack()
					_ = conn.Close()
				default:
					_, _ = w.Write([]byte(body))
				}
			}))
			defer server.Close()

			got, err := FetchModels(context.Background(), model.Channel{
				Type:     tt.typ,
				BaseUrls: []model.BaseUrl{{URL: server.URL + tt.basePath}},
				Keys:     []model.ChannelKey{{Enabled: true, ChannelKey: "k"}},
			})
			if (err != nil) != (tt.want == nil) || !slices.Equal(got, tt.want) {
				t.Fatalf("FetchModels() = %v, %v; want %v", got, err, tt.want)
			}
			if n := requests.Load(); n != tt.requests {
				t.Fatalf("upstream got %d requests, want %d", n, tt.requests)
			}
		})
	}
}

func TestFetchModelsUsesBrowserHeadersAndSummarizesHTMLError(t *testing.T) {
	observedUserAgent := ""
	observedAccept := ""
	observedAcceptLanguage := ""

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		observedUserAgent = r.Header.Get("User-Agent")
		observedAccept = r.Header.Get("Accept")
		observedAcceptLanguage = r.Header.Get("Accept-Language")
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`<!DOCTYPE html><html lang="en-US"><head><title>Just a moment...</title></head><body>blocked</body></html>`))
	}))
	defer server.Close()

	_, err := FetchModels(context.Background(), model.Channel{
		Type:     outbound.OutboundTypeOpenAIChat,
		BaseUrls: []model.BaseUrl{{URL: server.URL, Delay: 0}},
		Keys:     []model.ChannelKey{{Enabled: true, ChannelKey: "managed-key"}},
	})
	if err == nil {
		t.Fatalf("expected FetchModels to fail")
	}
	if !strings.Contains(err.Error(), "http 403: Just a moment...") {
		t.Fatalf("expected summarized HTML error, got %v", err)
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
