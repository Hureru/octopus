package sitesync

import (
	"fmt"
	"net/http"

	"github.com/bestruirui/octopus/internal/apperror"
	"github.com/bestruirui/octopus/internal/model"
)

const (
	CodeSiteSyncMissingGroupKey       = "site.sync.missing_group_key"
	CodeSiteSyncGroupModelsUnresolved = "site.sync.group_models_unresolved"
	CodeSiteSyncNoGroupResult         = "site.sync.no_group_result"
	CodeSiteSyncAllGroupsUnresolved   = "site.sync.all_groups_unresolved"
	CodeSiteSyncUnsupportedPlatform   = "site.sync.unsupported_platform"
	CodeSiteSyncSnapshotNil           = "site.sync.snapshot_nil"

	CodeSiteAuthAccessTokenRequired = "site.auth.access_token_required"
	CodeSiteAuthDirectTokenRequired = "site.auth.direct_token_required"
	CodeSiteAuthLoginFailed         = "site.auth.login_failed"
	CodeSiteAuthLoginTokenMissing   = "site.auth.login_token_missing"

	CodeSiteUpstreamHTTPError           = "site.upstream.http_error"
	CodeSiteUpstreamDecodeFailed        = "site.upstream.decode_failed"
	CodeSiteUpstreamCloudflareChallenge = "site.upstream.cloudflare_challenge"
)

func newMissingGroupKeyError(groupKey string) *apperror.Error {
	groupKey = model.NormalizeSiteGroupKey(groupKey)
	return apperror.Newf(
		CodeSiteSyncMissingGroupKey,
		"site sync requires a key for group %q; create a key for that group on the site and sync again",
		groupKey,
	).WithStatus(http.StatusBadRequest).WithParam("groupKey", groupKey)
}

func newUnsupportedSitePlatformError(platform model.SitePlatform) *apperror.Error {
	return apperror.Newf(
		CodeSiteSyncUnsupportedPlatform,
		"unsupported site platform: %s",
		platform,
	).WithStatus(http.StatusBadRequest).WithParam("platform", string(platform))
}

func newAccessTokenRequiredError() *apperror.Error {
	return apperror.New(CodeSiteAuthAccessTokenRequired, "access token is required").WithStatus(http.StatusBadRequest)
}

func newDirectTokenRequiredError() *apperror.Error {
	return apperror.New(CodeSiteAuthDirectTokenRequired, "direct token is required").WithStatus(http.StatusBadRequest)
}

func newSiteLoginFailedError(message string) *apperror.Error {
	if message == "" {
		message = "login failed"
	}
	return apperror.New(CodeSiteAuthLoginFailed, message).WithStatus(http.StatusBadGateway)
}

func newSiteLoginTokenMissingError() *apperror.Error {
	return apperror.New(CodeSiteAuthLoginTokenMissing, "login succeeded but no access token was returned").WithStatus(http.StatusBadGateway)
}

func newNoGroupResultError() *apperror.Error {
	return apperror.New(CodeSiteSyncNoGroupResult, "站点账号同步失败：没有可用的分组同步结果").WithStatus(http.StatusInternalServerError)
}

func newAllGroupsUnresolvedError(message string) *apperror.Error {
	if message == "" {
		message = "站点账号同步失败：所有分组都未能确认模型，已保留历史模型"
	}
	return apperror.New(CodeSiteSyncAllGroupsUnresolved, message).WithStatus(http.StatusBadGateway)
}

func newSnapshotNilError() *apperror.Error {
	return apperror.New(CodeSiteSyncSnapshotNil, "sync snapshot is nil").WithStatus(http.StatusInternalServerError)
}

func newSiteHTTPError(statusCode int, message string) *apperror.Error {
	if message == "" {
		message = fmt.Sprintf("http %d", statusCode)
	} else {
		message = fmt.Sprintf("http %d: %s", statusCode, message)
	}
	return apperror.New(CodeSiteUpstreamHTTPError, message).
		WithStatus(http.StatusBadGateway).
		WithParam("statusCode", statusCode)
}

func wrapSiteDecodeError(message string, err error) *apperror.Error {
	if message == "" {
		message = "decode response failed"
	}
	if err == nil {
		return apperror.New(CodeSiteUpstreamDecodeFailed, message).WithStatus(http.StatusBadGateway)
	}
	return apperror.Wrap(CodeSiteUpstreamDecodeFailed, message, err).WithStatus(http.StatusBadGateway)
}

func wrapCloudflareProtectionError(err *CloudflareProtectionError) *apperror.Error {
	if err == nil {
		return apperror.New(CodeSiteUpstreamCloudflareChallenge, "站点触发 Cloudflare 保护，请稍后重试，或手动访问站点完成验证/联系站点管理员放行").WithStatus(http.StatusServiceUnavailable)
	}
	return apperror.Wrap(CodeSiteUpstreamCloudflareChallenge, err.Error(), err).
		WithStatus(http.StatusServiceUnavailable).
		WithParam("statusCode", err.StatusCode)
}
