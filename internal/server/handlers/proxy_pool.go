package handlers

import (
	"net/http"
	"strconv"

	"github.com/bestruirui/octopus/internal/model"
	"github.com/bestruirui/octopus/internal/op"
	"github.com/bestruirui/octopus/internal/server/middleware"
	"github.com/bestruirui/octopus/internal/server/resp"
	"github.com/bestruirui/octopus/internal/server/router"
	"github.com/gin-gonic/gin"
)

func init() {
	router.NewGroupRouter("/api/v1/proxy-pool").
		Use(middleware.Auth()).
		AddRoute(router.NewRoute("/list", http.MethodGet).Handle(listProxyConfigurations)).
		AddRoute(router.NewRoute("/delete/:id", http.MethodDelete).Handle(deleteProxyConfiguration))

	router.NewGroupRouter("/api/v1/proxy-pool").
		Use(middleware.Auth()).
		Use(middleware.RequireJSON()).
		AddRoute(router.NewRoute("/create", http.MethodPost).Handle(createProxyConfiguration)).
		AddRoute(router.NewRoute("/update", http.MethodPost).Handle(updateProxyConfiguration)).
		AddRoute(router.NewRoute("/test", http.MethodPost).Handle(testProxyConfiguration))
}

func listProxyConfigurations(c *gin.Context) {
	items, err := op.ProxyConfigurationList(c.Request.Context())
	if err != nil {
		resp.Error(c, http.StatusInternalServerError, err.Error())
		return
	}
	resp.Success(c, items)
}

func createProxyConfiguration(c *gin.Context) {
	var item model.ProxyConfiguration
	if err := c.ShouldBindJSON(&item); err != nil {
		resp.InvalidJSON(c)
		return
	}
	if err := op.ProxyConfigurationCreate(&item, c.Request.Context()); err != nil {
		resp.Error(c, http.StatusBadRequest, err.Error())
		return
	}
	resp.Success(c, item)
}

func updateProxyConfiguration(c *gin.Context) {
	var req model.ProxyConfigurationUpdateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		resp.InvalidJSON(c)
		return
	}
	item, err := op.ProxyConfigurationUpdate(&req, c.Request.Context())
	if err != nil {
		resp.Error(c, http.StatusBadRequest, err.Error())
		return
	}
	resp.Success(c, item)
}

func deleteProxyConfiguration(c *gin.Context) {
	idNum, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		resp.InvalidParam(c)
		return
	}
	if err := op.ProxyConfigurationDelete(idNum, c.Request.Context()); err != nil {
		resp.Error(c, http.StatusBadRequest, err.Error())
		return
	}
	resp.Success(c, nil)
}

func testProxyConfiguration(c *gin.Context) {
	var req model.ProxyTestRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		resp.InvalidJSON(c)
		return
	}
	result, err := op.ProxyConfigurationTest(req, c.Request.Context())
	if err != nil {
		resp.Error(c, http.StatusInternalServerError, err.Error())
		return
	}
	resp.Success(c, result)
}
