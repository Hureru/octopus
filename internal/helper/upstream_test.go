package helper

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
)

func TestDoUpstreamRequestFindsAPIPrefix(t *testing.T) {
	// What the fake upstream serves at a path: the site's page, JSON labelled as text/html, a bare
	// status code, "hang" (the caller gives up while the upstream is still working), or (anything
	// else) that JSON body. A path that is not listed is a 404.
	const page, mislabelled, hang, ok = "<!doctype html><html>site</html>", "mislabelled", "hang", `{"ok":true}`
	const chat, responses = "/chat/completions", "/responses"
	// One step is one DoUpstreamRequest call; the steps of a case share the Base URL.
	type step struct {
		endpoint string // the request goes to Base URL + endpoint
		upstream map[string]string
		want     string   // "error", or the response: its status code if it is not 200, else its body
		paths    []string // upstream paths requested during the step, in order
	}
	atV1 := map[string]string{chat: page, "/v1" + chat: ok}
	tests := []struct {
		name     string
		basePath string
		stream   bool // the request body cannot be replayed
		steps    []step
	}{
		{"site root is replayed under /v1, then goes there directly", "", false, []step{
			{chat, atV1, ok, []string{chat, "/v1" + chat}},
			{chat, atV1, ok, []string{"/v1" + chat}},
			{chat, nil, "404", []string{"/v1" + chat}},
		}},
		{"api/v1 is found behind a sub path that answers 405", "/relay", false, []step{
			{chat, map[string]string{"/relay" + chat: "405", "/relay/api/v1" + chat: ok}, ok,
				[]string{"/relay" + chat, "/relay/v1" + chat, "/relay/api/v1" + chat}},
		}},
		{"versioned base URL is never replayed", "/v1", false, []step{
			{chat, nil, "404", []string{"/v1" + chat}},
		}},
		{"base URL that works as configured is never replayed afterwards", "", false, []step{
			{chat, map[string]string{chat: ok}, ok, []string{chat}},
			{chat, nil, "404", []string{chat}},
		}},
		{"a prefix learned for one endpoint is not applied to another", "", false, []step{
			{responses, map[string]string{responses: page, "/v1" + responses: ok}, ok, []string{responses, "/v1" + responses}},
			{chat, map[string]string{chat: ok}, ok, []string{chat}},
		}},
		{"JSON labelled as text/html was served, so it is not replayed", "", false, []step{
			{chat, map[string]string{chat: mislabelled, "/v1" + chat: ok}, ok, []string{chat}},
		}},
		{"no guess answers: the configured URL's response stands", "", false, []step{
			{chat, map[string]string{chat: page}, page, []string{chat, "/v1" + chat, "/api/v1" + chat}},
		}},
		{"an error under a guess is returned but not remembered", "", false, []step{
			{chat, map[string]string{chat: page, "/v1" + chat: "401"}, "401", []string{chat, "/v1" + chat}},
			{chat, atV1, ok, []string{chat, "/v1" + chat}},
		}},
		{"a replay the caller gave up on returns its error, not the site's page", "", false, []step{
			{chat, map[string]string{chat: page, "/v1" + chat: hang}, "error", []string{chat, "/v1" + chat}},
		}},
		{"a body that cannot be replayed is sent once", "", true, []step{
			{chat, atV1, page, []string{chat}},
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			learnedAPIPrefix.Clear() // a reused port must not inherit another case's prefix
			var mu sync.Mutex
			var upstream map[string]string
			var paths []string
			var giveUp context.CancelFunc
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if payload, _ := io.ReadAll(r.Body); string(payload) != "payload" {
					t.Errorf("%s got body %q, want the request's payload", r.URL.Path, payload)
				}
				mu.Lock()
				paths = append(paths, r.URL.Path)
				body, listed := upstream[r.URL.Path]
				cancel := giveUp
				mu.Unlock()
				status, _ := strconv.Atoi(body)
				switch {
				case !listed:
					http.NotFound(w, r)
				case body == hang:
					cancel()
					<-r.Context().Done()
				case status != 0:
					http.Error(w, body, status)
				case body == page:
					w.Header().Set("Content-Type", "text/html; charset=utf-8")
					_, _ = w.Write([]byte(page))
				case body == mislabelled:
					w.Header().Set("Content-Type", "text/html; charset=utf-8")
					_, _ = w.Write([]byte(ok))
				default:
					w.Header().Set("Content-Type", "application/json")
					_, _ = w.Write([]byte(body))
				}
			}))
			defer server.Close()
			baseURL := server.URL + tt.basePath

			for i, st := range tt.steps {
				ctx, cancel := context.WithCancel(context.Background())
				mu.Lock()
				upstream, paths, giveUp = st.upstream, nil, cancel
				mu.Unlock()
				var body io.Reader = strings.NewReader("payload")
				if tt.stream {
					body = io.NopCloser(body) // hides the type http.NewRequest knows how to replay
				}
				req, _ := http.NewRequestWithContext(ctx, http.MethodPost, baseURL+st.endpoint, body)
				got := "error"
				if resp, err := DoUpstreamRequest(server.Client(), req, baseURL); err == nil {
					data, _ := io.ReadAll(resp.Body)
					resp.Body.Close()
					got = string(data)
					if resp.StatusCode != http.StatusOK {
						got = strconv.Itoa(resp.StatusCode)
					}
				}
				cancel()
				mu.Lock()
				seen := slices.Clone(paths)
				mu.Unlock()
				if got != st.want || !slices.Equal(seen, st.paths) {
					t.Fatalf("step %d: got %q via %v, want %q via %v", i, got, seen, st.want, st.paths)
				}
			}
		})
	}
}
