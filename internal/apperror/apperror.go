package apperror

import (
	"errors"
	"fmt"
	"net/http"
)

const (
	CodeCommonInvalidJSON       = "common.invalid_json"
	CodeCommonInvalidParam      = "common.invalid_param"
	CodeCommonValidationFailed  = "common.validation_failed"
	CodeCommonBadRequest        = "common.bad_request"
	CodeCommonNotFound          = "common.not_found"
	CodeCommonDuplicateResource = "common.duplicate_resource"
	CodeCommonDatabaseError     = "common.database_error"
	CodeCommonInternalError     = "common.internal_error"

	CodeAuthUnauthorized       = "auth.unauthorized"
	CodeAuthForbidden          = "auth.forbidden"
	CodeAuthInvalidToken       = "auth.invalid_token"
	CodeAuthExpiredToken       = "auth.expired_token"
	CodeAuthInvalidCredentials = "auth.invalid_credentials"
	CodeAuthAPIKeyExpired      = "auth.api_key_expired"
	CodeAuthAPIKeyMissing      = "auth.api_key_missing"
	CodeAuthPasswordIncorrect  = "auth.password_incorrect"
	CodeAuthAPIKeyDisabled     = "auth.api_key_disabled"
	CodeAuthAPIKeyCostExceeded = "auth.api_key_cost_exceeded"
	CodeAuthAPIKeyRateLimited = "auth.api_key_rate_limited"

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
	Status  int
	Params  map[string]any
	Err     error
}

func New(code string, message string) *Error {
	return &Error{Code: code, Message: message}
}

func Newf(code string, format string, args ...any) *Error {
	return New(code, fmt.Sprintf(format, args...))
}

func Wrap(code string, message string, err error) *Error {
	return &Error{Code: code, Message: message, Err: err}
}

func Wrapf(code string, err error, format string, args ...any) *Error {
	return Wrap(code, fmt.Sprintf(format, args...), err)
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

func (e *Error) WithStatus(status int) *Error {
	if e == nil {
		return nil
	}
	e.Status = status
	return e
}

func (e *Error) WithParams(params map[string]any) *Error {
	if e == nil {
		return nil
	}
	if len(params) == 0 {
		e.Params = nil
		return e
	}
	e.Params = params
	return e
}

func (e *Error) WithParam(key string, value any) *Error {
	if e == nil {
		return nil
	}
	if key == "" {
		return e
	}
	if e.Params == nil {
		e.Params = make(map[string]any, 1)
	}
	e.Params[key] = value
	return e
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

func Status(err error) int {
	var appErr *Error
	if errors.As(err, &appErr) && appErr != nil {
		return appErr.Status
	}
	return 0
}

func Params(err error) map[string]any {
	var appErr *Error
	if errors.As(err, &appErr) && appErr != nil {
		return appErr.Params
	}
	return nil
}

func IsCode(err error, code string) bool {
	return Code(err) == code
}

func InvalidJSON(message string) *Error {
	if message == "" {
		message = "Invalid JSON format"
	}
	return New(CodeCommonInvalidJSON, message).WithStatus(http.StatusBadRequest)
}

func InvalidParam(message string) *Error {
	if message == "" {
		message = "Invalid parameter"
	}
	return New(CodeCommonInvalidParam, message).WithStatus(http.StatusBadRequest)
}
