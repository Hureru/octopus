package relay

import (
	"bytes"
	"context"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/bestruirui/octopus/internal/model"
	"github.com/bestruirui/octopus/internal/relay/bodycache"
	"github.com/bestruirui/octopus/internal/transformer/inbound"
	transformerModel "github.com/bestruirui/octopus/internal/transformer/model"
	"github.com/bestruirui/octopus/internal/transformer/outbound"
	"github.com/gin-gonic/gin"
)

// newSiteRootUpstream fakes a relay station whose API lives under /v1: any other path gets the
// station's web page with a 200, the way NewAPI answers. It returns the station's Base URL and a
// func listing the paths requested below it. The station is mounted under the test's name because
// the learned API prefix is process-wide and keyed by Base URL: a reused port must not carry one
// test's prefix into another.
func newSiteRootUpstream(t *testing.T, api http.HandlerFunc) (string, func() []string) {
	var mu sync.Mutex
	var paths []string
	mount := "/" + t.Name()
	mux := http.NewServeMux()
	mux.HandleFunc(mount+"/v1/", api)
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte("<!doctype html><html><body>station</body></html>"))
	})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		paths = append(paths, strings.TrimPrefix(r.URL.Path, mount))
		mu.Unlock()
		mux.ServeHTTP(w, r)
	}))
	t.Cleanup(server.Close)
	return server.URL + mount, func() []string {
		mu.Lock()
		defer mu.Unlock()
		return slices.Clone(paths)
	}
}

func TestForwardViaHTTPFindsAPIUnderSiteRoot(t *testing.T) {
	gin.SetMode(gin.TestMode)
	baseURL, paths := newSiteRootUpstream(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"resp_1","object":"response","created":1,"model":"gpt-4o","output":[{"type":"message","role":"assistant","content":[{"type":"output_text","text":"ok"}]}],"usage":{"input_tokens":1,"output_tokens":1,"total_tokens":2}}`))
	})
	channel := &model.Channel{
		Name:     "relay-site-root",
		Type:     outbound.OutboundTypeOpenAIResponse,
		BaseUrls: []model.BaseUrl{{URL: baseURL}},
		Keys:     []model.ChannelKey{{Enabled: true, ChannelKey: "k"}},
	}

	// The first request finds the API under /v1; the second one goes there directly.
	for _, want := range [][]string{{"/responses", "/v1/responses"}, {"/responses", "/v1/responses", "/v1/responses"}} {
		c, _ := gin.CreateTestContext(httptest.NewRecorder())
		c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
		internalReq := &transformerModel.InternalLLMRequest{Model: "gpt-4o", Stream: boolPtr(false), RawAPIFormat: transformerModel.APIFormatOpenAIResponse}
		req := &relayRequest{c: c, inAdapter: inbound.Get(inbound.InboundTypeOpenAIResponse), internalRequest: internalReq, metrics: NewRelayMetrics(1, "gpt-4o", nil, internalReq), apiKeyID: 1, requestModel: "gpt-4o"}
		ra := &relayAttempt{relayRequest: req, outAdapter: outbound.Get(channel.Type), channel: channel, usedKey: channel.Keys[0]}

		if statusCode, err := ra.forwardViaHTTP(context.Background()); err != nil || statusCode != http.StatusOK {
			t.Fatalf("forwardViaHTTP() = %d, %v; want 200", statusCode, err)
		}
		if got := paths(); !slices.Equal(got, want) {
			t.Fatalf("upstream paths = %v, want %v", got, want)
		}
	}
	if wsURL, _ := buildWSURL(baseURL); !strings.HasSuffix(wsURL, "/v1/responses") {
		t.Fatalf("buildWSURL() = %s, want the prefix learned over HTTP", wsURL)
	}
}

func TestImagesAttemptReplaysMultipartUnderSiteRoot(t *testing.T) {
	gin.SetMode(gin.TestMode)
	image := bytes.Repeat([]byte("png"), 100_000) // spans many reads, so a half-rebuilt body would show
	baseURL, paths := newSiteRootUpstream(t, func(w http.ResponseWriter, r *http.Request) {
		file, _, err := r.FormFile("image")
		if err != nil {
			t.Errorf("replayed request is not a readable multipart form: %v", err)
			return
		}
		if got, _ := io.ReadAll(file); !bytes.Equal(got, image) || r.FormValue("model") != "upstream-model" {
			t.Errorf("replayed form: %d image bytes, model %q; want %d bytes, model upstream-model", len(got), r.FormValue("model"), len(image))
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[{"url":"https://example.com/a.png"}]}`))
	})

	var form bytes.Buffer
	mw := multipart.NewWriter(&form)
	_ = mw.WriteField("model", "client-model")
	part, _ := mw.CreateFormFile("image", "a.png")
	_, _ = part.Write(image)
	_ = mw.Close()
	bc, err := bodycache.New(io.NopCloser(&form))
	if err != nil {
		t.Fatalf("bodycache.New failed: %v", err)
	}
	defer bc.Close()

	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/images/edits", nil)
	channel := &model.Channel{Type: outbound.OutboundTypeOpenAIChat, BaseUrls: []model.BaseUrl{{URL: baseURL}}}

	statusCode, _, _, _, err := imagesAttempt(context.Background(), "/images/edits", c, bc, true, mw.Boundary(), nil, false,
		channel, "k", 0, newImagesRelayMetrics(1, "client-model"), "upstream-model", nil)
	if err != nil || statusCode != http.StatusOK {
		t.Fatalf("imagesAttempt() = %d, %v; want 200", statusCode, err)
	}
	if got, want := paths(), []string{"/images/edits", "/v1/images/edits"}; !slices.Equal(got, want) {
		t.Fatalf("upstream paths = %v, want %v", got, want)
	}
	if got := recorder.Body.String(); got != `{"data":[{"url":"https://example.com/a.png"}]}` {
		t.Fatalf("client got %q, want the upstream's JSON", got)
	}
}
