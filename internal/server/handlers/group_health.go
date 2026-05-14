package handlers

import (
	"context"
	"errors"
	"net/http"
	"strconv"

	"github.com/bestruirui/octopus/internal/grouphealth"
	"github.com/bestruirui/octopus/internal/server/middleware"
	"github.com/bestruirui/octopus/internal/server/resp"
	"github.com/bestruirui/octopus/internal/server/router"
	"github.com/bestruirui/octopus/internal/utils/safe"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

var defaultGroupHealthService = grouphealth.NewService(nil, nil)

func init() {
	router.NewGroupRouter("/api/v1/group/health").
		Use(middleware.Auth()).
		AddRoute(
			router.NewRoute("/list", http.MethodGet).
				Handle(listGroupHealth),
		).
		AddRoute(
			router.NewRoute("/run-all", http.MethodPost).
				Handle(runAllGroupHealth),
		).
		AddRoute(
			router.NewRoute("/:id", http.MethodGet).
				Handle(getGroupHealth),
		).
		AddRoute(
			router.NewRoute("/:id/run", http.MethodPost).
				Handle(runGroupHealth),
		)
}

func listGroupHealth(c *gin.Context) {
	views, err := defaultGroupHealthService.ListGroupHealthViews(c.Request.Context())
	if err != nil {
		resp.Error(c, http.StatusInternalServerError, err.Error())
		return
	}
	resp.Success(c, views)
}

func getGroupHealth(c *gin.Context) {
	groupID, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		resp.InvalidParam(c)
		return
	}
	view, err := defaultGroupHealthService.GetGroupHealthViewByID(c.Request.Context(), groupID)
	if err != nil {
		resp.Error(c, http.StatusInternalServerError, err.Error())
		return
	}
	resp.Success(c, view)
}

func runGroupHealth(c *gin.Context) {
	groupID, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		resp.InvalidParam(c)
		return
	}
	running, err := defaultGroupHealthService.GetRunningSnapshotByGroupID(c.Request.Context(), groupID)
	if err == nil {
		c.JSON(http.StatusConflict, resp.ResponseStruct{
			Code:    http.StatusConflict,
			Message: grouphealth.ErrGroupHealthAlreadyRunning.Error(),
			Data:    running,
		})
		return
	}
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		resp.Error(c, http.StatusInternalServerError, err.Error())
		return
	}

	safe.Go("group-health-run", func() {
		runCtx := context.Background()
		_ = defaultGroupHealthService.RunGroupHealth(runCtx, groupID)
	})

	c.JSON(http.StatusAccepted, resp.ResponseStruct{
		Code:    http.StatusAccepted,
		Message: "accepted",
		Data: gin.H{
			"group_id": groupID,
		},
	})
}

func runAllGroupHealth(c *gin.Context) {
	safe.Go("group-health-run-all", func() {
		runCtx := context.Background()
		defaultGroupHealthService.RunAllGroupHealth(runCtx, 2)
	})

	c.JSON(http.StatusAccepted, resp.ResponseStruct{
		Code:    http.StatusAccepted,
		Message: "accepted",
		Data: gin.H{
			"all_groups": true,
		},
	})
}
