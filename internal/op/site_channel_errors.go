package op

import (
	"net/http"

	"github.com/bestruirui/octopus/internal/apperror"
)

const (
	CodeSiteChannelAccountNotFound         = "site_channel.account_not_found"
	CodeSiteChannelSiteNotFound            = "site_channel.site_not_found"
	CodeSiteChannelModelNotFound           = "site_channel.model_not_found"
	CodeSiteChannelRouteUpdateFailed       = "site_channel.route_update_failed"
	CodeSiteChannelModelDisableFailed      = "site_channel.model_disable_failed"
	CodeSiteChannelKeyCreateFailed         = "site_channel.key_create_failed"
	CodeSiteChannelSourceKeyUpdateFailed   = "site_channel.source_key_update_failed"
	CodeSiteChannelProjectedSettingsFailed = "site_channel.projected_settings_failed"
	CodeSiteChannelManualModelFailed       = "site_channel.manual_model_failed"
	CodeSiteChannelProjectFailed           = "site_channel.project_failed"
)

func newSiteChannelAccountNotFoundError() *apperror.Error {
	return apperror.New(CodeSiteChannelAccountNotFound, "site account not found").WithStatus(http.StatusNotFound)
}

func wrapSiteChannelRouteUpdateFailed(err error) *apperror.Error {
	return apperror.Wrap(CodeSiteChannelRouteUpdateFailed, "site channel route update failed", err).WithStatus(http.StatusInternalServerError)
}

func wrapSiteChannelModelDisableFailed(err error) *apperror.Error {
	return apperror.Wrap(CodeSiteChannelModelDisableFailed, "site channel model disable failed", err).WithStatus(http.StatusInternalServerError)
}

func wrapSiteChannelSourceKeyUpdateFailed(err error) *apperror.Error {
	return apperror.Wrap(CodeSiteChannelSourceKeyUpdateFailed, "site channel source key update failed", err).WithStatus(http.StatusInternalServerError)
}
