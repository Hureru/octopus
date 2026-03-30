package handlers

import (
	"context"
	"net/http"
	"strconv"
	"time"

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
		AddRoute(router.NewRoute("/:siteId/account/:accountId/projected-keys", http.MethodPut).Handle(updateSiteProjectedKeys)).
		AddRoute(router.NewRoute("/:siteId/account/:accountId/model-routes", http.MethodPut).Handle(updateSiteChannelModelRoutes)).
		AddRoute(router.NewRoute("/:siteId/account/:accountId/model-disabled", http.MethodPut).Handle(updateSiteChannelModelDisabled)).
		AddRoute(router.NewRoute("/:siteId/account/:accountId/model-routes/reset", http.MethodPost).Handle(resetSiteChannelModelRoutes))
}

func listSiteChannel(c *gin.Context) {
	data, err := op.SiteChannelList(c.Request.Context())
	if err != nil {
		resp.Error(c, http.StatusInternalServerError, err.Error())
		return
	}
	resp.Success(c, data)
}

func getSiteChannel(c *gin.Context) {
	siteID, err := strconv.Atoi(c.Param("siteId"))
	if err != nil {
		resp.Error(c, http.StatusBadRequest, resp.ErrInvalidParam)
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
		resp.Error(c, http.StatusBadRequest, resp.ErrInvalidJSON)
		return
	}
	if _, err := sitesvc.CreateAccountToken(c.Request.Context(), accountID, req); err != nil {
		resp.Error(c, http.StatusInternalServerError, err.Error())
		return
	}
	data, err := op.SiteChannelAccountGet(siteID, accountID, c.Request.Context())
	if err != nil {
		resp.Error(c, http.StatusInternalServerError, err.Error())
		return
	}
	resp.Success(c, data)
}

func updateSiteProjectedKeys(c *gin.Context) {
	siteID, accountID, ok := parseSiteChannelIDs(c)
	if !ok {
		return
	}
	var req model.SiteProjectedKeyUpdateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		resp.Error(c, http.StatusBadRequest, resp.ErrInvalidJSON)
		return
	}
	if err := op.UpdateSiteProjectedKeys(siteID, accountID, &req, c.Request.Context()); err != nil {
		resp.Error(c, http.StatusInternalServerError, err.Error())
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
		resp.Error(c, http.StatusBadRequest, resp.ErrInvalidJSON)
		return
	}
	for _, item := range req {
		if err := op.SiteModelRouteUpdate(accountID, item.GroupKey, item.ModelName, item.RouteType, model.SiteModelRouteSourceManualOverride, true, item.RouteRawPayload, c.Request.Context()); err != nil {
			resp.Error(c, http.StatusInternalServerError, err.Error())
			return
		}
	}
	if err := reprojectSiteChannelAccount(c.Request.Context(), accountID); err != nil {
		resp.Error(c, http.StatusInternalServerError, err.Error())
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
		resp.Error(c, http.StatusBadRequest, resp.ErrInvalidJSON)
		return
	}
	for _, item := range req {
		if err := op.SiteModelDisabledUpdate(accountID, item.GroupKey, item.ModelName, item.Disabled, c.Request.Context()); err != nil {
			resp.Error(c, http.StatusInternalServerError, err.Error())
			return
		}
	}
	if err := reprojectSiteChannelAccount(c.Request.Context(), accountID); err != nil {
		resp.Error(c, http.StatusInternalServerError, err.Error())
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
		resp.Error(c, http.StatusInternalServerError, err.Error())
		return
	}
	if err := reprojectSiteChannelAccount(c.Request.Context(), accountID); err != nil {
		resp.Error(c, http.StatusInternalServerError, err.Error())
		return
	}
	data, err := op.SiteChannelAccountGet(siteID, accountID, c.Request.Context())
	if err != nil {
		resp.Error(c, http.StatusInternalServerError, err.Error())
		return
	}
	resp.Success(c, data)
}

func parseSiteChannelIDs(c *gin.Context) (int, int, bool) {
	siteID, err := strconv.Atoi(c.Param("siteId"))
	if err != nil {
		resp.Error(c, http.StatusBadRequest, resp.ErrInvalidParam)
		return 0, 0, false
	}
	accountID, err := strconv.Atoi(c.Param("accountId"))
	if err != nil {
		resp.Error(c, http.StatusBadRequest, resp.ErrInvalidParam)
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
