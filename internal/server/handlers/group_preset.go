package handlers

import (
	"net/http"
	"strconv"

	"github.com/bestruirui/octopus/internal/apperror"
	"github.com/bestruirui/octopus/internal/model"
	"github.com/bestruirui/octopus/internal/op"
	"github.com/bestruirui/octopus/internal/server/middleware"
	"github.com/bestruirui/octopus/internal/server/resp"
	"github.com/bestruirui/octopus/internal/server/router"
	"github.com/dlclark/regexp2"
	"github.com/gin-gonic/gin"
)

func init() {
	router.NewGroupRouter("/api/v1/group").
		Use(middleware.Auth()).
		Use(middleware.RequireJSON()).
		AddRoute(
			router.NewRoute("/preset/list/:groupID", http.MethodGet).
				Handle(listGroupPresets),
		).
		AddRoute(
			router.NewRoute("/preset/create/:groupID", http.MethodPost).
				Handle(createGroupPreset),
		).
		AddRoute(
			router.NewRoute("/preset/activate/:id", http.MethodPost).
				Handle(activateGroupPreset),
		).
		AddRoute(
			router.NewRoute("/preset/overwrite/:id", http.MethodPost).
				Handle(overwriteGroupPreset),
		).
		AddRoute(
			router.NewRoute("/preset/update/:id", http.MethodPut).
				Handle(updateGroupPreset),
		).
		AddRoute(
			router.NewRoute("/preset/rename/:id", http.MethodPut).
				Handle(renameGroupPreset),
		).
		AddRoute(
			router.NewRoute("/preset/delete/:id", http.MethodDelete).
				Handle(deleteGroupPreset),
		).
		AddRoute(
			router.NewRoute("/pin/:id", http.MethodPost).
				Handle(setGroupPin),
		)
}

func parseIDParam(c *gin.Context, name string) (int, bool) {
	idStr := c.Param(name)
	id, err := strconv.Atoi(idStr)
	if err != nil || id <= 0 {
		resp.InvalidParam(c)
		return 0, false
	}
	return id, true
}

func listGroupPresets(c *gin.Context) {
	groupID, ok := parseIDParam(c, "groupID")
	if !ok {
		return
	}
	presets, err := op.GroupPresetList(groupID, c.Request.Context())
	if err != nil {
		resp.ErrorWithAppError(c, http.StatusInternalServerError, groupError(codeGroupPresetListFailed, "group preset list failed", err))
		return
	}
	resp.Success(c, presets)
}

func createGroupPreset(c *gin.Context) {
	groupID, ok := parseIDParam(c, "groupID")
	if !ok {
		return
	}
	var req model.GroupPresetCreateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		resp.InvalidJSON(c)
		return
	}
	preset, err := op.GroupPresetCreate(groupID, req.Name, c.Request.Context())
	if err != nil {
		resp.ErrorWithAppError(c, http.StatusInternalServerError, groupError(codeGroupPresetCreateFailed, "group preset create failed", err))
		return
	}
	resp.Success(c, preset)
}

func activateGroupPreset(c *gin.Context) {
	id, ok := parseIDParam(c, "id")
	if !ok {
		return
	}
	if err := op.GroupPresetActivate(id, c.Request.Context()); err != nil {
		resp.ErrorWithAppError(c, http.StatusInternalServerError, groupError(codeGroupPresetActivateFailed, "group preset activate failed", err))
		return
	}
	resp.Success(c, "group preset activated")
}

func overwriteGroupPreset(c *gin.Context) {
	id, ok := parseIDParam(c, "id")
	if !ok {
		return
	}
	preset, err := op.GroupPresetOverwrite(id, c.Request.Context())
	if err != nil {
		resp.ErrorWithAppError(c, http.StatusInternalServerError, groupError(codeGroupPresetOverwriteFailed, "group preset overwrite failed", err))
		return
	}
	resp.Success(c, preset)
}

func updateGroupPreset(c *gin.Context) {
	id, ok := parseIDParam(c, "id")
	if !ok {
		return
	}
	var req model.GroupPresetUpdateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		resp.InvalidJSON(c)
		return
	}
	if req.MatchRegex != nil && *req.MatchRegex != "" {
		if _, err := regexp2.Compile(*req.MatchRegex, regexp2.ECMAScript); err != nil {
			resp.ErrorWithAppError(c, http.StatusBadRequest, apperror.New(apperror.CodeCommonValidationFailed, err.Error()).WithStatus(http.StatusBadRequest))
			return
		}
	}
	preset, err := op.GroupPresetUpdate(id, &req, c.Request.Context())
	if err != nil {
		resp.ErrorWithAppError(c, http.StatusInternalServerError, groupError(codeGroupPresetUpdateFailed, "group preset update failed", err))
		return
	}
	resp.Success(c, preset)
}

func renameGroupPreset(c *gin.Context) {
	id, ok := parseIDParam(c, "id")
	if !ok {
		return
	}
	var req model.GroupPresetRenameRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		resp.InvalidJSON(c)
		return
	}
	if err := op.GroupPresetRename(id, req.Name, c.Request.Context()); err != nil {
		resp.ErrorWithAppError(c, http.StatusInternalServerError, groupError(codeGroupPresetRenameFailed, "group preset rename failed", err))
		return
	}
	resp.Success(c, "group preset renamed")
}

func deleteGroupPreset(c *gin.Context) {
	id, ok := parseIDParam(c, "id")
	if !ok {
		return
	}
	if err := op.GroupPresetDelete(id, c.Request.Context()); err != nil {
		resp.ErrorWithAppError(c, http.StatusInternalServerError, groupError(codeGroupPresetDeleteFailed, "group preset delete failed", err))
		return
	}
	resp.Success(c, "group preset deleted")
}

func setGroupPin(c *gin.Context) {
	id, ok := parseIDParam(c, "id")
	if !ok {
		return
	}
	var req model.GroupPinRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		resp.InvalidJSON(c)
		return
	}
	if err := op.GroupSetPinned(id, req.Pinned, c.Request.Context()); err != nil {
		resp.ErrorWithAppError(c, http.StatusInternalServerError, groupError(codeGroupPinFailed, "group pin failed", err))
		return
	}
	resp.Success(c, "group pin updated")
}
