package sitesync

import (
	"context"
	"net/http"
	"strings"

	"github.com/bestruirui/octopus/internal/op"
)

type verificationBrowserRequestFunc func(
	context.Context,
	op.VerificationBrowserRequestInput,
) (*op.VerificationBrowserResponse, error)

type verificationBrowserTransport struct {
	binding op.VerificationBrowserBinding
	request verificationBrowserRequestFunc
}

type verificationBrowserTransportContextKey struct{}

func withVerificationBrowserTransport(
	ctx context.Context,
	binding op.VerificationBrowserBinding,
	request verificationBrowserRequestFunc,
) context.Context {
	if request == nil {
		request = op.VerificationBrowserRequest
	}
	return context.WithValue(
		ctx,
		verificationBrowserTransportContextKey{},
		verificationBrowserTransport{binding: binding, request: request},
	)
}

func verificationBrowserTransportFromContext(
	ctx context.Context,
) (verificationBrowserTransport, bool) {
	if ctx == nil {
		return verificationBrowserTransport{}, false
	}
	transport, ok := ctx.Value(
		verificationBrowserTransportContextKey{},
	).(verificationBrowserTransport)
	return transport, ok && transport.request != nil
}

func verificationBrowserHeaders(header http.Header) map[string]string {
	result := make(map[string]string)
	for name, values := range header {
		if verificationBrowserHeaderForbidden(name) {
			continue
		}
		value := strings.TrimSpace(strings.Join(values, ", "))
		if value != "" {
			result[name] = value
		}
	}
	return result
}

func verificationBrowserHeaderForbidden(name string) bool {
	name = strings.ToLower(strings.TrimSpace(name))
	switch name {
	case "",
		"accept-charset",
		"accept-encoding",
		"access-control-request-headers",
		"access-control-request-method",
		"connection",
		"content-length",
		"cookie",
		"date",
		"dnt",
		"expect",
		"host",
		"keep-alive",
		"origin",
		"permissions-policy",
		"referer",
		"te",
		"trailer",
		"transfer-encoding",
		"upgrade",
		"user-agent",
		"via":
		return true
	default:
		return strings.HasPrefix(name, "proxy-") ||
			strings.HasPrefix(name, "sec-") ||
			strings.HasPrefix(name, "x-http-method")
	}
}

func verificationBrowserResponseHeader(values map[string]string) http.Header {
	header := make(http.Header, len(values))
	for name, value := range values {
		name = strings.TrimSpace(name)
		if name != "" {
			header.Set(name, value)
		}
	}
	return header
}
