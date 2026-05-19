package handlers

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strconv"

	"github.com/bestruirui/octopus/internal/grouphealth"
	"github.com/bestruirui/octopus/internal/model"
	"github.com/bestruirui/octopus/internal/op"
	"github.com/bestruirui/octopus/internal/server/middleware"
	"github.com/bestruirui/octopus/internal/server/resp"
	"github.com/bestruirui/octopus/internal/server/router"
	"github.com/bestruirui/octopus/internal/utils/safe"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

var defaultGroupHealthService = grouphealth.NewService(nil, nil)

type groupHealthRunRequest struct {
	ProbeMode model.GroupHealthProbeMode `json:"probe_mode"`
}

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

func ensureGroupHealthEnabled(c *gin.Context) bool {
	enabled, err := op.SettingGetBool(model.SettingKeyGroupHealthEnabled)
	if err != nil {
		resp.Error(c, http.StatusInternalServerError, err.Error())
		return false
	}
	if !enabled {
		resp.Error(c, http.StatusForbidden, "group health checks are disabled")
		return false
	}
	return true
}

func listGroupHealth(c *gin.Context) {
	if !ensureGroupHealthEnabled(c) {
		return
	}
	views, err := defaultGroupHealthService.ListGroupHealthViews(c.Request.Context())
	if err != nil {
		resp.Error(c, http.StatusInternalServerError, err.Error())
		return
	}
	resp.Success(c, views)
}

func getGroupHealth(c *gin.Context) {
	if !ensureGroupHealthEnabled(c) {
		return
	}
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

func parseGroupHealthRunRequest(c *gin.Context) (model.GroupHealthProbeMode, error) {
	if c.Request.Body == nil || c.Request.ContentLength == 0 {
		return model.GroupHealthProbeModeStandard, nil
	}

	var req groupHealthRunRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		if errors.Is(err, io.EOF) {
			return model.GroupHealthProbeModeStandard, nil
		}
		return "", err
	}

	switch req.ProbeMode {
	case "", model.GroupHealthProbeModeStandard:
		return model.GroupHealthProbeModeStandard, nil
	case model.GroupHealthProbeModeFull:
		return model.GroupHealthProbeModeFull, nil
	default:
		return "", errors.New("invalid probe_mode")
	}
}

func runGroupHealth(c *gin.Context) {
	if !ensureGroupHealthEnabled(c) {
		return
	}
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

	probeMode, err := parseGroupHealthRunRequest(c)
	if err != nil {
		resp.InvalidParam(c)
		return
	}

	safe.Go("group-health-run", func() {
		runCtx := context.Background()
		_ = defaultGroupHealthService.RunGroupHealth(runCtx, groupID, probeMode)
	})

	c.JSON(http.StatusAccepted, resp.ResponseStruct{
		Code:    http.StatusAccepted,
		Message: "accepted",
		Data: gin.H{
			"group_id":   groupID,
			"probe_mode": probeMode,
		},
	})
}

func runAllGroupHealth(c *gin.Context) {
	if !ensureGroupHealthEnabled(c) {
		return
	}

	probeMode, err := parseGroupHealthRunRequest(c)
	if err != nil {
		resp.InvalidParam(c)
		return
	}
	safe.Go("group-health-run-all", func() {
		runCtx := context.Background()
		defaultGroupHealthService.RunAllGroupHealth(runCtx, 2, probeMode)
	})

	c.JSON(http.StatusAccepted, resp.ResponseStruct{
		Code:    http.StatusAccepted,
		Message: "accepted",
		Data: gin.H{
			"all_groups": true,
			"probe_mode": probeMode,
		},
	})
}
