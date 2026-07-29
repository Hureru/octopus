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
	router.NewGroupRouter("/api/v1/header-policy").
		Use(middleware.Auth()).
		AddRoute(router.NewRoute("/list", http.MethodGet).Handle(listHeaderPolicies)).
		AddRoute(router.NewRoute("/registry", http.MethodGet).Handle(getHeaderPolicyRegistry)).
		AddRoute(router.NewRoute("/user-agents", http.MethodGet).Handle(listUserAgentProfiles)).
		AddRoute(router.NewRoute("/preview", http.MethodGet).Handle(previewHeaderPolicy)).
		AddRoute(router.NewRoute("/:id", http.MethodDelete).Handle(deleteHeaderPolicy))

	router.NewGroupRouter("/api/v1/header-policy").
		Use(middleware.Auth()).
		Use(middleware.RequireJSON()).
		AddRoute(router.NewRoute("/upsert", http.MethodPost).Handle(upsertHeaderPolicy)).
		AddRoute(router.NewRoute("/user-agent", http.MethodPost).Handle(upsertUserAgentProfile))
}

func listHeaderPolicies(c *gin.Context) {
	items, err := op.HeaderPolicyList(c.Request.Context())
	if err != nil {
		resp.Error(c, http.StatusInternalServerError, err.Error())
		return
	}
	resp.Success(c, items)
}

func getHeaderPolicyRegistry(c *gin.Context) {
	resp.Success(c, op.HeaderPolicyRegistry())
}

func upsertHeaderPolicy(c *gin.Context) {
	var request model.HeaderPolicy
	if err := c.ShouldBindJSON(&request); err != nil {
		resp.InvalidJSON(c)
		return
	}
	item, err := op.HeaderPolicyUpsert(c.Request.Context(), request)
	if err != nil {
		resp.Error(c, http.StatusBadRequest, err.Error())
		return
	}
	resp.Success(c, item)
}

func deleteHeaderPolicy(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil || id <= 0 {
		resp.InvalidParam(c)
		return
	}
	if err := op.HeaderPolicyDelete(c.Request.Context(), id); err != nil {
		resp.Error(c, http.StatusBadRequest, err.Error())
		return
	}
	resp.Success(c, nil)
}

func previewHeaderPolicy(c *gin.Context) {
	channelID, _ := strconv.Atoi(c.Query("channel_id"))
	canonicalID, _ := strconv.Atoi(c.Query("canonical_model_id"))
	candidateID, _ := strconv.Atoi(c.Query("route_candidate_id"))
	item, err := op.ResolveHeaderPolicy(c.Request.Context(), channelID, canonicalID, candidateID)
	if err != nil {
		resp.Error(c, http.StatusInternalServerError, err.Error())
		return
	}
	resp.Success(c, item)
}

func listUserAgentProfiles(c *gin.Context) {
	items, err := op.UserAgentProfileList(c.Request.Context())
	if err != nil {
		resp.Error(c, http.StatusInternalServerError, err.Error())
		return
	}
	resp.Success(c, items)
}

func upsertUserAgentProfile(c *gin.Context) {
	var request model.UserAgentProfile
	if err := c.ShouldBindJSON(&request); err != nil {
		resp.InvalidJSON(c)
		return
	}
	item, err := op.UserAgentProfileUpsert(c.Request.Context(), request)
	if err != nil {
		resp.Error(c, http.StatusBadRequest, err.Error())
		return
	}
	resp.Success(c, item)
}
