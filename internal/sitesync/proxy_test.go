package sitesync

import (
	"testing"

	"github.com/bestruirui/octopus/internal/model"
)

func TestResolveSiteAccountProxyPrefersAccountProxy(t *testing.T) {
	accountProxy := "socks5://127.0.0.1:7891"
	siteProxy := "socks5://127.0.0.1:7890"

	useProxy, proxyURL := resolveSiteAccountProxy(&model.Site{
		Proxy:     false,
		SiteProxy: &siteProxy,
	}, &model.SiteAccount{
		AccountProxy: &accountProxy,
	})

	if !useProxy {
		t.Fatalf("expected proxy to be enabled when account proxy is configured")
	}
	if proxyURL == nil || *proxyURL != accountProxy {
		t.Fatalf("expected account proxy %q, got %#v", accountProxy, proxyURL)
	}
}

func TestResolveSiteAccountProxyFallsBackToSiteSettings(t *testing.T) {
	siteProxy := "socks5://127.0.0.1:7890"

	useProxy, proxyURL := resolveSiteAccountProxy(&model.Site{
		Proxy:     true,
		SiteProxy: &siteProxy,
	})

	if !useProxy {
		t.Fatalf("expected site proxy to be enabled")
	}
	if proxyURL == nil || *proxyURL != siteProxy {
		t.Fatalf("expected site proxy %q, got %#v", siteProxy, proxyURL)
	}
}

func TestResolveSiteAccountProxyDisablesProxyWhenNoConfigExists(t *testing.T) {
	useProxy, proxyURL := resolveSiteAccountProxy(&model.Site{
		Proxy: false,
	})

	if useProxy {
		t.Fatalf("expected proxy to be disabled when neither account nor site proxy is enabled")
	}
	if proxyURL != nil {
		t.Fatalf("expected no proxy URL, got %#v", proxyURL)
	}
}

func TestBuildManagedAuthHeadersUsesCookieThenBearerFallback(t *testing.T) {
	headers := buildManagedAuthHeaders("sid=cookie-session")
	if len(headers) != 2 {
		t.Fatalf("expected two auth header candidates, got %d", len(headers))
	}
	if headers[0]["Cookie"] != "sid=cookie-session" {
		t.Fatalf("expected cookie header candidate first, got %#v", headers[0])
	}
	if headers[1]["Authorization"] != "Bearer sid=cookie-session" {
		t.Fatalf("expected bearer fallback candidate second, got %#v", headers[1])
	}
}

func TestBuildManagedAuthHeadersUsesBearerOnlyForPlainToken(t *testing.T) {
	headers := buildManagedAuthHeaders("plain-token")
	if len(headers) != 1 {
		t.Fatalf("expected one auth header candidate, got %d", len(headers))
	}
	if headers[0]["Authorization"] != "Bearer plain-token" {
		t.Fatalf("expected bearer header for plain token, got %#v", headers[0])
	}
}

func TestLooksLikeCookieToken(t *testing.T) {
	cases := []struct {
		name  string
		token string
		want  bool
	}{
		{name: "cookie-pair", token: "sid=cookie-session", want: true},
		{name: "cookie-chain", token: "sid=a; theme=dark", want: true},
		{name: "bearer-token", token: "Bearer plain-token", want: false},
		{name: "plain-token", token: "plain-token", want: false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := looksLikeCookieToken(tc.token); got != tc.want {
				t.Fatalf("looksLikeCookieToken(%q) = %v, want %v", tc.token, got, tc.want)
			}
		})
	}
}
