package apperror

import (
	"errors"
	"fmt"
)

const (
	CodeSiteSub2APIAPIKeyRequired      = "site.sub2api.api_key_required"
	CodeSiteSub2APIModelAPIKeyRequired = "site.sub2api.model_api_key_required"
	CodeSiteSub2APIEnvelopeFailed      = "site.sub2api.envelope_failed"
	CodeSiteSub2APIMissingData         = "site.sub2api.missing_data"
)

// Error carries a stable machine-readable code plus a default human-readable message.
// The default message is intended as a fallback; UI clients should translate by Code.
type Error struct {
	Code    string
	Message string
	Err     error
}

func New(code string, message string) *Error {
	return &Error{Code: code, Message: message}
}

func Wrap(code string, message string, err error) *Error {
	return &Error{Code: code, Message: message, Err: err}
}

func (e *Error) Error() string {
	if e == nil {
		return ""
	}
	if e.Message != "" {
		return e.Message
	}
	if e.Err != nil {
		return e.Err.Error()
	}
	return e.Code
}

func (e *Error) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}

func Code(err error) string {
	var appErr *Error
	if errors.As(err, &appErr) && appErr != nil {
		return appErr.Code
	}
	return ""
}

func Message(err error) string {
	if err == nil {
		return ""
	}
	var appErr *Error
	if errors.As(err, &appErr) && appErr != nil {
		return appErr.Error()
	}
	return err.Error()
}

func IsCode(err error, code string) bool {
	return Code(err) == code
}

func Newf(code string, format string, args ...any) *Error {
	return New(code, fmt.Sprintf(format, args...))
}
