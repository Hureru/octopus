package helper

import (
	"bufio"
	"bytes"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"

	"github.com/bestruirui/octopus/internal/utils/log"
)

// learnedAPIPrefix maps a Base URL without an API version to the path prefix its API answers
// under: "" when it works as configured, "/v1" or "/api/v1" when it turned out to be a site root.
// shortcut: an entry lives until restart, so an upstream that moves its API needs one; expire entries if that bites.
var learnedAPIPrefix sync.Map

// LearnedAPIPrefix returns the prefix DoUpstreamRequest learned for baseURL, "" if none.
func LearnedAPIPrefix(baseURL string) string {
	value, _ := learnedAPIPrefix.Load(baseURL)
	prefix, _ := value.(string)
	return prefix
}

// DoUpstreamRequest sends a request whose URL is baseURL plus an endpoint path. A Base URL
// without an API version may be a site root, so when the answer shows the endpoint is not there
// (404, 405 or the site's HTML page) the request is replayed under the usual API prefixes. Such an
// answer means the request was not served, so nothing runs twice. The first success settles the
// prefix for that Base URL; from then on every request is sent exactly once.
func DoUpstreamRequest(client *http.Client, req *http.Request, baseURL string) (*http.Response, error) {
	base, err := url.Parse(strings.TrimSuffix(baseURL, "/"))
	// The request URL is checked, not baseURL: Gemini requests get their version added later.
	// Host+Path because apiVersionSegment needs the character in front of the version's slash.
	if err != nil || apiVersionSegment.MatchString(req.URL.Host+req.URL.Path) || !strings.HasPrefix(req.URL.Path, base.Path) {
		return client.Do(req)
	}
	endpoint := req.URL.Path[len(base.Path):]
	if prefix, known := learnedAPIPrefix.Load(baseURL); known {
		req.URL.Path = base.Path + prefix.(string) + endpoint
		return client.Do(req)
	}
	resp, err := client.Do(req)
	if err != nil {
		return resp, err
	}
	if !endpointLooksMissing(resp) {
		if resp.StatusCode < 300 {
			learnedAPIPrefix.Store(baseURL, "")
		}
		return resp, nil
	}
	// A body that cannot be rebuilt is sent once.
	if req.GetBody == nil {
		return resp, nil
	}
	for _, prefix := range []string{"/v1", "/api/v1"} {
		retry := req.Clone(req.Context())
		retry.URL.Path = base.Path + prefix + endpoint
		if retry.Body, err = req.GetBody(); err != nil {
			break
		}
		alt, err := client.Do(retry)
		if err != nil {
			// No HTTP response at all: other paths on the same host will not fare better.
			break
		}
		if endpointLooksMissing(alt) {
			alt.Body.Close()
			continue
		}
		resp.Body.Close()
		if alt.StatusCode < 300 {
			// Only a success is remembered: an error under a guessed path is worth reporting,
			// but it does not prove the API lives there.
			learnedAPIPrefix.Store(baseURL, prefix)
			log.Infof("base url %s%s has no API version; its API answers under %s, requests go there from now on", base.Host, base.Path, prefix)
		}
		return alt, nil
	}
	// A guess counts only when an endpoint answers it; otherwise the configured URL's answer stands.
	return resp, nil
}

// endpointLooksMissing reports whether resp reads as "no API at this path": 404/405, or the
// site's own HTML page, which NewAPI-style stations serve with 200 for any unknown path.
func endpointLooksMissing(resp *http.Response) bool {
	switch resp.StatusCode {
	case http.StatusNotFound, http.StatusMethodNotAllowed:
		return true
	}
	if resp.StatusCode >= 300 || !strings.HasPrefix(strings.ToLower(resp.Header.Get("Content-Type")), "text/html") {
		return false
	}
	// Some upstreams label JSON as text/html. That is an answer that was served (and billed),
	// so only real markup counts as the site's page.
	body := bufio.NewReader(resp.Body)
	resp.Body = struct {
		io.Reader
		io.Closer
	}{body, resp.Body}
	head, _ := body.Peek(64)
	return bytes.HasPrefix(bytes.TrimLeft(head, "\ufeff \t\r\n"), []byte("<"))
}
