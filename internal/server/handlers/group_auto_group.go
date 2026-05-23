package handlers

import (
	"net/http"

	"github.com/bestruirui/octopus/internal/model"
	"github.com/bestruirui/octopus/internal/op"
	"github.com/bestruirui/octopus/internal/server/middleware"
	"github.com/bestruirui/octopus/internal/server/resp"
	"github.com/bestruirui/octopus/internal/server/router"
	"github.com/gin-gonic/gin"
)

func init() {
	router.NewGroupRouter("/api/v1/group/auto-group").
		Use(middleware.Auth()).
		Use(middleware.RequireJSON()).
		AddRoute(router.NewRoute("/config", http.MethodGet).Handle(getGroupAutoGroupConfig)).
		AddRoute(router.NewRoute("/config", http.MethodPut).Handle(updateGroupAutoGroupConfig)).
		AddRoute(router.NewRoute("/run", http.MethodPost).Handle(runGroupAutoGroup))
}

func getGroupAutoGroupConfig(c *gin.Context) {
	config, err := op.GroupAutoGroupConfigGet(c.Request.Context())
	if err != nil {
		resp.Error(c, http.StatusInternalServerError, err.Error())
		return
	}
	resp.Success(c, config)
}

func updateGroupAutoGroupConfig(c *gin.Context) {
	var req model.GroupAutoGroupConfigUpdateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		resp.InvalidJSON(c)
		return
	}
	config, err := op.GroupAutoGroupConfigUpdate(&req, c.Request.Context())
	if err != nil {
		resp.ErrorWithAppError(c, http.StatusInternalServerError, err)
		return
	}
	resp.Success(c, config)
}

func runGroupAutoGroup(c *gin.Context) {
	var req model.GroupAutoGroupRunRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		resp.InvalidJSON(c)
		return
	}
	if err := op.RunGroupAutoGroup(req.ChannelIDs, c.Request.Context()); err != nil {
		resp.ErrorWithAppError(c, http.StatusInternalServerError, err)
		return
	}
	resp.Success(c, nil)
}
