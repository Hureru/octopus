package handlers

import (
	"net/http"

	"github.com/bestruirui/octopus/internal/apperror"
)

func channelError(code string, message string, err error) *apperror.Error {
	return apperror.Wrap(code, message, err).WithStatus(http.StatusInternalServerError)
}

func groupError(code string, message string, err error) *apperror.Error {
	return apperror.Wrap(code, message, err).WithStatus(http.StatusInternalServerError)
}

func modelError(code string, message string, err error) *apperror.Error {
	return apperror.Wrap(code, message, err).WithStatus(http.StatusInternalServerError)
}

const (
	codeChannelNotFound          = "channel.not_found"
	codeChannelCreateFailed      = "channel.create_failed"
	codeChannelUpdateFailed      = "channel.update_failed"
	codeChannelDeleteFailed      = "channel.delete_failed"
	codeChannelFetchModelsFailed = "channel.fetch_models_failed"

	codeGroupNotFound     = "group.not_found"
	codeGroupCreateFailed = "group.create_failed"
	codeGroupUpdateFailed = "group.update_failed"
	codeGroupDeleteFailed = "group.delete_failed"
	codeGroupPinFailed    = "group.pin_failed"

	codeGroupPresetListFailed        = "group.preset.list_failed"
	codeGroupPresetCreateFailed      = "group.preset.create_failed"
	codeGroupPresetCreateBlankFailed = "group.preset.create_blank_failed"
	codeGroupPresetCloneFailed       = "group.preset.clone_failed"
	codeGroupPresetUpdateFailed      = "group.preset.update_failed"
	codeGroupPresetDeleteFailed      = "group.preset.delete_failed"
	codeGroupPresetActivateFailed    = "group.preset.activate_failed"

	codeModelPriceUpdateFailed = "model.price_update_failed"
	codeModelPriceDeleteFailed = "model.price_delete_failed"
	codeModelCreateFailed      = "model.create_failed"
	codeModelUpdateFailed      = "model.update_failed"
	codeModelDeleteFailed      = "model.delete_failed"
)
