package relay

import (
	"net/http"
	"testing"

	dbmodel "github.com/bestruirui/octopus/internal/model"
)

// 占位符只能取到会被转发的客户端头：凭据类的头在 hopByHopHeaders 里，引用它的头必须被丢弃——
// 既不能是网关自己的 Key，也不能是出站头里已经写好的上游渠道 Key。
func TestCustomHeaderPlaceholderCannotReachCredentials(t *testing.T) {
	client := http.Header{}
	client.Set("Authorization", "Bearer sk-octopus-secret")
	client.Set("X-Api-Key", "sk-octopus-secret")
	client.Set("User-Agent", "codex/1.2")
	channel := &dbmodel.Channel{CustomHeader: []dbmodel.CustomHeader{
		{HeaderKey: "X-Client-UA", HeaderValue: "{client_header:User-Agent}"},
		{HeaderKey: "X-Leak-Auth", HeaderValue: "{client_header:Authorization}"},
		{HeaderKey: "X-Leak-Key", HeaderValue: "{client_header:X-Api-Key}"},
	}}

	// 主转发路径上，出站头在拷贝客户端头之前就带着上游凭据。
	proxied := http.Header{"Authorization": {"Bearer upstream-key"}}
	copyProxyHeaders(client, channel, proxied)
	ws := buildUpstreamWSHeaders(client, channel, "upstream-key")

	for name, headers := range map[string]http.Header{"compact": proxied, "ws": ws} {
		if got := headers.Get("X-Client-UA"); got != "codex/1.2" {
			t.Errorf("%s: X-Client-UA = %q, want the client User-Agent", name, got)
		}
		for _, leaked := range []string{"X-Leak-Auth", "X-Leak-Key"} {
			if _, ok := headers[leaked]; ok {
				t.Errorf("%s: %s = %q, want the header dropped", name, leaked, headers.Get(leaked))
			}
		}
	}
}

// 前面的自定义 Header 覆盖了同名的客户端头，后面的占位符仍然要读到客户端原值。
func TestCustomHeaderPlaceholderReadsClientValueAfterOverride(t *testing.T) {
	client := http.Header{}
	client.Set("X-Session", "client-value")
	channel := &dbmodel.Channel{CustomHeader: []dbmodel.CustomHeader{
		{HeaderKey: "X-Session", HeaderValue: "fixed"},
		{HeaderKey: "X-Copy", HeaderValue: "{client_header:X-Session}"},
	}}

	proxied := http.Header{}
	copyProxyHeaders(client, channel, proxied)
	ws := buildUpstreamWSHeaders(client, channel, "upstream-key")

	for name, headers := range map[string]http.Header{"compact": proxied, "ws": ws} {
		if got := headers.Get("X-Session"); got != "fixed" {
			t.Errorf("%s: X-Session = %q, want fixed", name, got)
		}
		if got := headers.Get("X-Copy"); got != "client-value" {
			t.Errorf("%s: X-Copy = %q, want client-value", name, got)
		}
	}
}
