package relay

import (
	"net/http"
	"testing"

	dbmodel "github.com/bestruirui/octopus/internal/model"
)

// 占位符只能取到会被转发的客户端头：网关自己的凭据在 hopByHopHeaders 里，引用它的头必须被丢弃。
func TestCustomHeaderPlaceholderCannotReachGatewayCredentials(t *testing.T) {
	client := http.Header{}
	client.Set("Authorization", "Bearer sk-octopus-secret")
	client.Set("X-Api-Key", "sk-octopus-secret")
	client.Set("User-Agent", "codex/1.2")
	channel := &dbmodel.Channel{CustomHeader: []dbmodel.CustomHeader{
		{HeaderKey: "X-Client-UA", HeaderValue: "{client_header:User-Agent}"},
		{HeaderKey: "X-Leak-Auth", HeaderValue: "{client_header:Authorization}"},
		{HeaderKey: "X-Leak-Key", HeaderValue: "{client_header:X-Api-Key}"},
	}}

	proxied := http.Header{}
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
