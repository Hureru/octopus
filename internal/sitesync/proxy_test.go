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
