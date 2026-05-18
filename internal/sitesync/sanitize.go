package sitesync

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/bestruirui/octopus/internal/apperror"
)

const maxSiteStatusMessageRunes = 300

var (
	bearerTokenPattern     = regexp.MustCompile(`(?i)Bearer\s+[A-Za-z0-9._~+/=-]+`)
	openAITokenPattern     = regexp.MustCompile(`\bsk-[A-Za-z0-9_-]{8,}\b`)
	sensitiveKVPattern     = regexp.MustCompile(`(?i)\b(access_token|api_key|refresh_token|token|authorization|cookie)=([^\s&;]+)`)
	htmlTitlePattern       = regexp.MustCompile(`(?is)<title>\s*([^<]+?)\s*</title>`)
	multiWhitespacePattern = regexp.MustCompile(`\s+`)
	likelyHTMLPattern      = regexp.MustCompile(`(?is)<\s*!doctype\s+html|<\s*html\b|<\s*body\b|<\s*script\b`)
	statusCodePattern      = regexp.MustCompile(`\b(?:http\s*)?(401|403|408|429|5\d\d)\b`)
)

func sanitizeSiteStatusMessage(err error) string {
	if err == nil {
		return ""
	}
	return sanitizeSiteStatusText(apperror.Message(err))
}

func sanitizeSiteStatusText(message string) string {
	message = strings.TrimSpace(message)
	if message == "" {
		return ""
	}
	if isLikelyHTML(message) {
		return truncateSiteStatusMessage(summarizeHTMLForStatus(message))
	}
	if summary := embeddedHTMLSummaryForStatus(message); summary != "" {
		return truncateSiteStatusMessage(summary)
	}
	message = stripControlCharacters(message)
	message = maskSensitiveSiteText(message)
	message = multiWhitespacePattern.ReplaceAllString(message, " ")
	return truncateSiteStatusMessage(strings.TrimSpace(message))
}

func sanitizeSiteError(err error) error {
	if err == nil {
		return nil
	}
	message := sanitizeSiteStatusMessage(err)
	if message == "" {
		message = "站点操作失败"
	}
	code := apperror.Code(err)
	if code == "" {
		code = apperror.CodeCommonInternalError
	}
	return apperror.Wrap(code, message, err).WithStatus(apperror.Status(err)).WithParams(apperror.Params(err))
}

func siteBatchReason(err error) SiteBatchReason {
	if err == nil {
		return SiteBatchReasonUnknown
	}
	if errors.Is(err, context.Canceled) {
		return SiteBatchReasonContextCanceled
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return SiteBatchReasonContextDeadlineExceeded
	}
	code := apperror.Code(err)
	switch code {
	case CodeSiteUpstreamCloudflareChallenge:
		return SiteBatchReasonCloudflareProtection
	case CodeSiteAuthLoginFailed:
		return SiteBatchReasonLoginFailed
	case CodeSiteAuthAccessTokenRequired:
		return SiteBatchReasonAccessTokenRequired
	case CodeSiteAuthDirectTokenRequired, apperror.CodeSiteSub2APIAPIKeyRequired, apperror.CodeSiteSub2APIModelAPIKeyRequired:
		return SiteBatchReasonDirectTokenRequired
	case CodeSiteSyncUnsupportedPlatform:
		return SiteBatchReasonUnsupportedPlatform
	case CodeSiteSyncMissingGroupKey:
		return SiteBatchReasonMissingGroupKey
	case CodeSiteUpstreamDecodeFailed:
		return SiteBatchReasonUpstreamDecodeFailed
	case CodeSiteUpstreamHTTPError:
		if status := siteErrorStatusCode(err); status == 401 || status == 403 {
			return SiteBatchReasonUnauthorized
		}
		msg := strings.ToLower(apperror.Message(err))
		if strings.Contains(msg, "html") {
			return SiteBatchReasonUpstreamHTMLResponse
		}
		return SiteBatchReasonUpstreamHTTPError
	case apperror.CodeCommonDatabaseError:
		return SiteBatchReasonDatabaseError
	}
	lowered := strings.ToLower(apperror.Message(err))
	switch {
	case strings.Contains(lowered, "cloudflare") || strings.Contains(lowered, "just a moment"):
		return SiteBatchReasonCloudflareProtection
	case strings.Contains(lowered, "unauthorized") || strings.Contains(lowered, "forbidden") || strings.Contains(lowered, "invalid token") || strings.Contains(lowered, "未登录") || strings.Contains(lowered, "登录") || strings.Contains(lowered, "过期"):
		return SiteBatchReasonUnauthorized
	case strings.Contains(lowered, "deadline exceeded"):
		return SiteBatchReasonContextDeadlineExceeded
	case strings.Contains(lowered, "context canceled") || strings.Contains(lowered, "cancelled"):
		return SiteBatchReasonContextCanceled
	case strings.Contains(lowered, "timeout") || strings.Contains(lowered, "timed out"):
		return SiteBatchReasonTimeout
	case isLikelyHTML(lowered):
		return SiteBatchReasonUpstreamHTMLResponse
	default:
		return SiteBatchReasonUnknown
	}
}

func siteErrorStatusCode(err error) int {
	params := apperror.Params(err)
	if params != nil {
		switch value := params["statusCode"].(type) {
		case int:
			return value
		case int64:
			return int(value)
		case float64:
			return int(value)
		case string:
			var parsed int
			if _, scanErr := fmt.Sscanf(value, "%d", &parsed); scanErr == nil {
				return parsed
			}
		}
	}
	if match := statusCodePattern.FindStringSubmatch(apperror.Message(err)); len(match) >= 2 {
		var parsed int
		if _, scanErr := fmt.Sscanf(match[1], "%d", &parsed); scanErr == nil {
			return parsed
		}
	}
	return 0
}

func isLikelyHTML(text string) bool {
	return likelyHTMLPattern.MatchString(text)
}

func embeddedHTMLSummaryForStatus(text string) string {
	lowered := strings.ToLower(text)
	idx := strings.Index(lowered, "<!doctype")
	if idx < 0 {
		idx = strings.Index(lowered, "<html")
	}
	if idx < 0 {
		return ""
	}
	prefix := strings.TrimSpace(text[:idx])
	prefix = stripControlCharacters(maskSensitiveSiteText(prefix))
	prefix = strings.TrimSpace(multiWhitespacePattern.ReplaceAllString(prefix, " "))
	summary := summarizeHTMLForStatus(text[idx:])
	if prefix == "" {
		return summary
	}
	return prefix + ": " + summary
}

func summarizeHTMLForStatus(text string) string {
	lowered := strings.ToLower(text)
	if strings.Contains(lowered, "cloudflare") || strings.Contains(lowered, "just a moment") || strings.Contains(lowered, "cf-error-code") || strings.Contains(lowered, "cloudflare ray id") {
		return "站点触发 Cloudflare 保护，请稍后重试，或手动访问站点完成验证/联系站点管理员放行"
	}
	if summary := anyRouterExtractHTMLErrorSummary(text); summary != "" {
		return "上游返回 HTML 页面：" + sanitizeHTMLTitle(summary)
	}
	if match := htmlTitlePattern.FindStringSubmatch(text); len(match) >= 2 {
		return "上游返回 HTML 页面：" + sanitizeHTMLTitle(match[1])
	}
	return "上游返回 HTML 页面，无法解析为接口响应"
}

func sanitizeHTMLTitle(title string) string {
	title = stripControlCharacters(title)
	title = maskSensitiveSiteText(title)
	title = strings.TrimSpace(multiWhitespacePattern.ReplaceAllString(title, " "))
	if pipe := strings.Index(title, "|"); pipe >= 0 {
		title = strings.TrimSpace(title[:pipe])
	}
	if title == "" {
		return "无法解析为接口响应"
	}
	return truncateSiteStatusMessage(title)
}

func stripControlCharacters(text string) string {
	var b strings.Builder
	b.Grow(len(text))
	for _, r := range text {
		if r == '\n' || r == '\r' || r == '\t' || !unicode.IsControl(r) {
			b.WriteRune(r)
		}
	}
	return b.String()
}

func maskSensitiveSiteText(text string) string {
	text = bearerTokenPattern.ReplaceAllString(text, "Bearer [redacted]")
	text = openAITokenPattern.ReplaceAllString(text, "sk-[redacted]")
	text = sensitiveKVPattern.ReplaceAllString(text, "$1=[redacted]")
	return text
}

func truncateSiteStatusMessage(text string) string {
	text = strings.TrimSpace(text)
	if text == "" || utf8.RuneCountInString(text) <= maxSiteStatusMessageRunes {
		return text
	}
	suffix := "...[truncated]"
	available := maxSiteStatusMessageRunes - utf8.RuneCountInString(suffix)
	if available < 0 {
		available = 0
	}
	runes := []rune(text)
	return string(runes[:available]) + suffix
}
