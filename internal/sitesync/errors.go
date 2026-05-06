package sitesync

import (
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"
)

type CloudflareProtectionError struct {
	StatusCode int
	RetryAfter time.Duration
	Message    string
}

func (e *CloudflareProtectionError) Error() string {
	if e == nil {
		return ""
	}
	if e.RetryAfter > 0 {
		return fmt.Sprintf("http %d: %s，建议 %s 后重试", e.StatusCode, e.Message, e.RetryAfter)
	}
	return fmt.Sprintf("http %d: %s", e.StatusCode, e.Message)
}

func IsCloudflareProtectionError(err error) bool {
	var cfErr *CloudflareProtectionError
	return errors.As(err, &cfErr)
}

func CloudflareRetryAfter(err error) time.Duration {
	var cfErr *CloudflareProtectionError
	if errors.As(err, &cfErr) && cfErr != nil {
		return cfErr.RetryAfter
	}
	return 0
}

func newCloudflareProtectionError(statusCode int, header http.Header) *CloudflareProtectionError {
	return &CloudflareProtectionError{
		StatusCode: statusCode,
		RetryAfter: parseSiteRetryAfter(header.Get("Retry-After")),
		Message:    "站点触发 Cloudflare 保护，请稍后重试，或手动访问站点完成验证/联系站点管理员放行",
	}
}

func parseSiteRetryAfter(header string) time.Duration {
	if header == "" {
		return 0
	}
	secs, err := strconv.Atoi(strings.TrimSpace(header))
	if err != nil || secs <= 0 {
		return 0
	}
	delay := time.Duration(secs) * time.Second
	if delay > 60*time.Second {
		return 60 * time.Second
	}
	return delay
}
