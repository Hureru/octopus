package grouphealth

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/bestruirui/octopus/internal/model"
	"github.com/bestruirui/octopus/internal/transformer/outbound"
)

func TestBuildProbeRequestForResponses(t *testing.T) {
	channel := &model.Channel{
		Type:     outbound.OutboundTypeOpenAIResponse,
		BaseUrls: []model.BaseUrl{{URL: "https://example.com/v1"}},
	}
	usedKey := &model.ChannelKey{ID: 1, ChannelKey: "sk-test"}

	req, err := buildProbeRequest(context.Background(), channel, usedKey, "gpt-5.4")
	if err != nil {
		t.Fatalf("buildProbeRequest returned error: %v", err)
	}
	if req.URL.Path != "/v1/responses" {
		t.Fatalf("expected /v1/responses, got %s", req.URL.Path)
	}
}

func TestBuildProbeRequestForEmbeddings(t *testing.T) {
	channel := &model.Channel{
		Type:     outbound.OutboundTypeOpenAIEmbedding,
		BaseUrls: []model.BaseUrl{{URL: "https://example.com/v1"}},
	}
	usedKey := &model.ChannelKey{ID: 1, ChannelKey: "sk-test"}

	req, err := buildProbeRequest(context.Background(), channel, usedKey, "text-embedding-3-large")
	if err != nil {
		t.Fatalf("buildProbeRequest returned error: %v", err)
	}
	if req.URL.Path != "/v1/embeddings" {
		t.Fatalf("expected /v1/embeddings, got %s", req.URL.Path)
	}
}

func TestRunCandidateProbesTheAPIUnderASiteRoot(t *testing.T) {
	// The station serves its web page for unknown paths; the API under /v1 rejects the key.
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/station/v1/chat/completions" {
			http.Error(w, `{"error":"invalid key"}`, http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte("<!doctype html><html><body>station</body></html>"))
	}))
	defer server.Close()

	// The path keeps this Base URL apart from any other test's: learned API prefixes are process-wide.
	channel := model.Channel{Type: outbound.OutboundTypeOpenAIChat, BaseUrls: []model.BaseUrl{{URL: server.URL + "/station"}}}
	result := NewProber().RunCandidate(context.Background(), channel, model.ChannelKey{ID: 1, ChannelKey: "sk-test"}, "gpt-5")
	if result.Success || result.HTTPStatus != http.StatusUnauthorized {
		t.Fatalf("probe = success %t, status %d; want the API's 401, not the web page's 200", result.Success, result.HTTPStatus)
	}
}
