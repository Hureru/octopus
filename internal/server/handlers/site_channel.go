package handlers

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/bestruirui/octopus/internal/apperror"
	"github.com/bestruirui/octopus/internal/model"
	"github.com/bestruirui/octopus/internal/op"
	"github.com/bestruirui/octopus/internal/server/middleware"
	"github.com/bestruirui/octopus/internal/server/resp"
	"github.com/bestruirui/octopus/internal/server/router"
	sitesvc "github.com/bestruirui/octopus/internal/site"
	"github.com/gin-gonic/gin"
)

func init() {
	router.NewGroupRouter("/api/v1/site-channel").
		Use(middleware.Auth()).
		AddRoute(router.NewRoute("/list", http.MethodGet).Handle(listSiteChannel)).
		AddRoute(router.NewRoute("/:siteId", http.MethodGet).Handle(getSiteChannel)).
		AddRoute(router.NewRoute("/:siteId/account/:accountId", http.MethodGet).Handle(getSiteChannelAccount)).
		AddRoute(router.NewRoute("/:siteId/account/:accountId/model-history", http.MethodGet).Handle(getSiteChannelModelHistory))

	router.NewGroupRouter("/api/v1/site-channel").
		Use(middleware.Auth()).
		Use(middleware.RequireJSON()).
		AddRoute(router.NewRoute("/:siteId/account/:accountId/keys", http.MethodPost).Handle(createSiteChannelKey)).
		AddRoute(router.NewRoute("/:siteId/account/:accountId/source-keys", http.MethodPut).Handle(updateSiteSourceKeys)).
		AddRoute(router.NewRoute("/:siteId/account/:accountId/model-routes", http.MethodPut).Handle(updateSiteChannelModelRoutes)).
		AddRoute(router.NewRoute("/:siteId/account/:accountId/model-disabled", http.MethodPut).Handle(updateSiteChannelModelDisabled)).
		AddRoute(router.NewRoute("/:siteId/account/:accountId/projected-channel-settings", http.MethodPut).Handle(updateSiteProjectedChannelSettings)).
		AddRoute(router.NewRoute("/:siteId/account/:accountId/manual-models", http.MethodPost).Handle(addSiteManualModels)).
		AddRoute(router.NewRoute("/:siteId/account/:accountId/manual-models/delete", http.MethodPost).Handle(deleteSiteManualModel)).
		AddRoute(router.NewRoute("/:siteId/account/:accountId/model-routes/reset", http.MethodPost).Handle(resetSiteChannelModelRoutes))
}

func listSiteChannel(c *gin.Context) {
	includeHistory, err := parseBoolQuery(c, "include_history", true)
	if err != nil {
		resp.Error(c, http.StatusBadRequest, err.Error())
		return
	}
	data, err := op.SiteChannelListWithOptions(c.Request.Context(), op.SiteChannelListOptions{IncludeHistory: includeHistory})
	if err != nil {
		resp.Error(c, http.StatusInternalServerError, err.Error())
		return
	}
	resp.Success(c, data)
}

func getSiteChannel(c *gin.Context) {
	siteID, err := strconv.Atoi(c.Param("siteId"))
	if err != nil {
		resp.InvalidParam(c)
		return
	}
	data, err := op.SiteChannelGet(siteID, c.Request.Context())
	if err != nil {
		resp.Error(c, http.StatusInternalServerError, err.Error())
		return
	}
	resp.Success(c, data)
}

func getSiteChannelAccount(c *gin.Context) {
	siteID, accountID, ok := parseSiteChannelIDs(c)
	if !ok {
		return
	}
	data, err := op.SiteChannelAccountGet(siteID, accountID, c.Request.Context())
	if err != nil {
		resp.Error(c, http.StatusInternalServerError, err.Error())
		return
	}
	resp.Success(c, data)
}

func getSiteChannelModelHistory(c *gin.Context) {
	siteID, accountID, ok := parseSiteChannelIDs(c)
	if !ok {
		return
	}
	data, err := op.SiteChannelModelHistory(siteID, accountID, c.Request.Context())
	if err != nil {
		resp.Error(c, http.StatusInternalServerError, err.Error())
		return
	}
	resp.Success(c, data)
}

func createSiteChannelKey(c *gin.Context) {
	siteID, accountID, ok := parseSiteChannelIDs(c)
	if !ok {
		return
	}
	var req model.SiteChannelKeyCreateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		resp.InvalidJSON(c)
		return
	}
	if _, err := sitesvc.CreateAccountToken(c.Request.Context(), accountID, req); err != nil {
		resp.ErrorWithAppError(c, http.StatusInternalServerError, apperror.Wrap(op.CodeSiteChannelKeyCreateFailed, "site channel key create failed", err).WithStatus(http.StatusInternalServerError))
		return
	}
	data, err := op.SiteChannelAccountGet(siteID, accountID, c.Request.Context())
	if err != nil {
		resp.Error(c, http.StatusInternalServerError, err.Error())
		return
	}
	resp.Success(c, data)
}

func updateSiteSourceKeys(c *gin.Context) {
	siteID, accountID, ok := parseSiteChannelIDs(c)
	if !ok {
		return
	}
	var req model.SiteSourceKeyUpdateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		resp.InvalidJSON(c)
		return
	}
	if err := op.UpdateSiteSourceKeys(siteID, accountID, &req, c.Request.Context()); err != nil {
		resp.ErrorWithAppError(c, http.StatusInternalServerError, apperror.Wrap(op.CodeSiteChannelSourceKeyUpdateFailed, "site channel source key update failed", err).WithStatus(http.StatusInternalServerError))
		return
	}
	if err := reprojectSiteChannelAccount(c.Request.Context(), accountID); err != nil {
		resp.ErrorWithAppError(c, http.StatusInternalServerError, apperror.Wrap(op.CodeSiteChannelProjectFailed, "site channel project failed", err).WithStatus(http.StatusInternalServerError))
		return
	}
	data, err := op.SiteChannelAccountGet(siteID, accountID, c.Request.Context())
	if err != nil {
		resp.Error(c, http.StatusInternalServerError, err.Error())
		return
	}
	resp.Success(c, data)
}

func updateSiteChannelModelRoutes(c *gin.Context) {
	siteID, accountID, ok := parseSiteChannelIDs(c)
	if !ok {
		return
	}
	var req []model.SiteModelRouteUpdateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		resp.InvalidJSON(c)
		return
	}
	for _, item := range req {
		if err := op.SiteModelRouteUpdate(accountID, item.GroupKey, item.ModelName, item.RouteType, model.SiteModelRouteSourceManualOverride, true, item.RouteRawPayload, c.Request.Context()); err != nil {
			resp.ErrorWithAppError(c, http.StatusInternalServerError, apperror.Wrap(op.CodeSiteChannelRouteUpdateFailed, "site channel route update failed", err).WithStatus(http.StatusInternalServerError))
			return
		}
	}
	if err := reprojectSiteChannelAccount(c.Request.Context(), accountID); err != nil {
		resp.ErrorWithAppError(c, http.StatusInternalServerError, apperror.Wrap(op.CodeSiteChannelProjectFailed, "site channel project failed", err).WithStatus(http.StatusInternalServerError))
		return
	}
	data, err := op.SiteChannelAccountGet(siteID, accountID, c.Request.Context())
	if err != nil {
		resp.Error(c, http.StatusInternalServerError, err.Error())
		return
	}
	resp.Success(c, data)
}

func updateSiteChannelModelDisabled(c *gin.Context) {
	siteID, accountID, ok := parseSiteChannelIDs(c)
	if !ok {
		return
	}
	var req []model.SiteModelDisableUpdateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		resp.InvalidJSON(c)
		return
	}
	for _, item := range req {
		if err := op.SiteModelDisabledUpdate(accountID, item.GroupKey, item.ModelName, item.Disabled, c.Request.Context()); err != nil {
			resp.ErrorWithAppError(c, http.StatusInternalServerError, apperror.Wrap(op.CodeSiteChannelModelDisableFailed, "site channel model disable failed", err).WithStatus(http.StatusInternalServerError))
			return
		}
	}
	if err := reprojectSiteChannelAccount(c.Request.Context(), accountID); err != nil {
		resp.ErrorWithAppError(c, http.StatusInternalServerError, apperror.Wrap(op.CodeSiteChannelProjectFailed, "site channel project failed", err).WithStatus(http.StatusInternalServerError))
		return
	}
	data, err := op.SiteChannelAccountGet(siteID, accountID, c.Request.Context())
	if err != nil {
		resp.Error(c, http.StatusInternalServerError, err.Error())
		return
	}
	resp.Success(c, data)
}

func updateSiteProjectedChannelSettings(c *gin.Context) {
	siteID, accountID, ok := parseSiteChannelIDs(c)
	if !ok {
		return
	}
	var req []model.SiteProjectedChannelSettingsUpdateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		resp.InvalidJSON(c)
		return
	}
	if err := op.UpdateSiteProjectedChannelSettings(siteID, accountID, req, c.Request.Context()); err != nil {
		status := siteChannelMutationErrorStatus(err)
		resp.ErrorWithAppError(c, status, apperror.Wrap(op.CodeSiteChannelProjectedSettingsFailed, "site projected channel settings update failed", err).WithStatus(status))
		return
	}
	data, err := op.SiteChannelAccountGet(siteID, accountID, c.Request.Context())
	if err != nil {
		resp.Error(c, http.StatusInternalServerError, err.Error())
		return
	}
	resp.Success(c, data)
}

func addSiteManualModels(c *gin.Context) {
	siteID, accountID, ok := parseSiteChannelIDs(c)
	if !ok {
		return
	}
	var req model.SiteManualModelAddRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		resp.InvalidJSON(c)
		return
	}
	if err := op.SiteManualModelsAdd(siteID, accountID, &req, c.Request.Context()); err != nil {
		status := siteChannelMutationErrorStatus(err)
		resp.ErrorWithAppError(c, status, apperror.Wrap(op.CodeSiteChannelManualModelFailed, "site manual model update failed", err).WithStatus(status))
		return
	}
	if err := reprojectSiteChannelAccount(c.Request.Context(), accountID); err != nil {
		resp.ErrorWithAppError(c, http.StatusInternalServerError, apperror.Wrap(op.CodeSiteChannelProjectFailed, "site channel project failed", err).WithStatus(http.StatusInternalServerError))
		return
	}
	data, err := op.SiteChannelAccountGet(siteID, accountID, c.Request.Context())
	if err != nil {
		resp.Error(c, http.StatusInternalServerError, err.Error())
		return
	}
	resp.Success(c, data)
}

func deleteSiteManualModel(c *gin.Context) {
	siteID, accountID, ok := parseSiteChannelIDs(c)
	if !ok {
		return
	}
	var req model.SiteManualModelDeleteRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		resp.InvalidJSON(c)
		return
	}
	if err := op.SiteManualModelDelete(siteID, accountID, &req, c.Request.Context()); err != nil {
		status := siteChannelMutationErrorStatus(err)
		resp.ErrorWithAppError(c, status, apperror.Wrap(op.CodeSiteChannelManualModelFailed, "site manual model update failed", err).WithStatus(status))
		return
	}
	if err := reprojectSiteChannelAccount(c.Request.Context(), accountID); err != nil {
		resp.ErrorWithAppError(c, http.StatusInternalServerError, apperror.Wrap(op.CodeSiteChannelProjectFailed, "site channel project failed", err).WithStatus(http.StatusInternalServerError))
		return
	}
	data, err := op.SiteChannelAccountGet(siteID, accountID, c.Request.Context())
	if err != nil {
		resp.Error(c, http.StatusInternalServerError, err.Error())
		return
	}
	resp.Success(c, data)
}

func resetSiteChannelModelRoutes(c *gin.Context) {
	siteID, accountID, ok := parseSiteChannelIDs(c)
	if !ok {
		return
	}
	if err := op.SiteChannelResetAccountRoutes(siteID, accountID, c.Request.Context()); err != nil {
		resp.ErrorWithAppError(c, http.StatusInternalServerError, apperror.Wrap(op.CodeSiteChannelRouteUpdateFailed, "site channel route update failed", err).WithStatus(http.StatusInternalServerError))
		return
	}
	if err := reprojectSiteChannelAccount(c.Request.Context(), accountID); err != nil {
		resp.ErrorWithAppError(c, http.StatusInternalServerError, apperror.Wrap(op.CodeSiteChannelProjectFailed, "site channel project failed", err).WithStatus(http.StatusInternalServerError))
		return
	}
	data, err := op.SiteChannelAccountGet(siteID, accountID, c.Request.Context())
	if err != nil {
		resp.Error(c, http.StatusInternalServerError, err.Error())
		return
	}
	resp.Success(c, data)
}

func siteChannelMutationErrorStatus(err error) int {
	var appErr *apperror.Error
	if errors.As(err, &appErr) && appErr != nil && appErr.Status > 0 {
		return appErr.Status
	}
	message := strings.ToLower(err.Error())
	switch {
	case strings.Contains(message, "not found"):
		return http.StatusNotFound
	case strings.Contains(message, "required"),
		strings.Contains(message, "invalid"),
		strings.Contains(message, "duplicate"),
		strings.Contains(message, "already exists"),
		strings.Contains(message, "json object"),
		strings.Contains(message, "unsupported"):
		return http.StatusBadRequest
	default:
		return http.StatusInternalServerError
	}
}

func parseSiteChannelIDs(c *gin.Context) (int, int, bool) {
	siteID, err := strconv.Atoi(c.Param("siteId"))
	if err != nil {
		resp.InvalidParam(c)
		return 0, 0, false
	}
	accountID, err := strconv.Atoi(c.Param("accountId"))
	if err != nil {
		resp.InvalidParam(c)
		return 0, 0, false
	}
	return siteID, accountID, true
}

func reprojectSiteChannelAccount(parent context.Context, accountID int) error {
	ctx, cancel := context.WithTimeout(parent, 5*time.Minute)
	defer cancel()

	_, err := sitesvc.ProjectAccount(ctx, accountID)
	return err
}
