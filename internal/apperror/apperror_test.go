package apperror

import (
	"errors"
	"net/http"
	"testing"
)

func TestErrorAccessors(t *testing.T) {
	err := New("site.sync.missing_group_key", "missing key for group default").
		WithStatus(http.StatusBadRequest).
		WithParam("groupKey", "default")

	if got := Code(err); got != "site.sync.missing_group_key" {
		t.Fatalf("Code() = %q", got)
	}
	if got := Message(err); got != "missing key for group default" {
		t.Fatalf("Message() = %q", got)
	}
	if got := Status(err); got != http.StatusBadRequest {
		t.Fatalf("Status() = %d", got)
	}
	if got := Params(err)["groupKey"]; got != "default" {
		t.Fatalf("Params()[groupKey] = %#v", got)
	}
}

func TestWrappedErrorAccessors(t *testing.T) {
	base := errors.New("upstream failed")
	err := Wrap(CodeCommonInternalError, "wrapped upstream failed", base).
		WithStatus(http.StatusBadGateway).
		WithParams(map[string]any{"statusCode": 502})

	if !errors.Is(err, base) {
		t.Fatalf("wrapped error should match base error")
	}
	if got := Code(err); got != CodeCommonInternalError {
		t.Fatalf("Code() = %q", got)
	}
	if got := Status(err); got != http.StatusBadGateway {
		t.Fatalf("Status() = %d", got)
	}
	if got := Params(err)["statusCode"]; got != 502 {
		t.Fatalf("Params()[statusCode] = %#v", got)
	}
}

func TestPlainErrorAccessors(t *testing.T) {
	err := errors.New("plain")
	if got := Code(err); got != "" {
		t.Fatalf("Code() = %q", got)
	}
	if got := Message(err); got != "plain" {
		t.Fatalf("Message() = %q", got)
	}
	if got := Status(err); got != 0 {
		t.Fatalf("Status() = %d", got)
	}
	if got := Params(err); got != nil {
		t.Fatalf("Params() = %#v", got)
	}
}
