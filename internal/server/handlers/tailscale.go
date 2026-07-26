package handlers

import (
	"errors"
	"net/http"

	"github.com/bestruirui/octopus/internal/server/middleware"
	"github.com/bestruirui/octopus/internal/server/resp"
	"github.com/bestruirui/octopus/internal/server/router"
	tailscaletunnel "github.com/bestruirui/octopus/internal/tailscale"
	"github.com/gin-gonic/gin"
)

func init() {
	router.NewGroupRouter("/api/v1/tailscale").
		Use(middleware.Auth()).
		AddRoute(

			router.NewRoute("/status", http.MethodGet).
				Handle(getTailscaleStatus),
		).
		AddRoute(

			router.NewRoute("/start", http.MethodPost).
				Handle(startTailscaleFunnel),
		).
		AddRoute(

			router.NewRoute("/stop", http.MethodPost).
				Handle(stopTailscaleFunnel),
		).
		AddRoute(

			router.NewRoute("/install", http.MethodPost).
				Handle(installTailscale),
		).
		AddRoute(

			router.NewRoute("/login", http.MethodPost).
				Handle(loginTailscale),
		)
}

func getTailscaleStatus(c *gin.Context) {
	resp.Success(c, tailscaletunnel.GetStatus(c.Request.Context()))
}

func startTailscaleFunnel(c *gin.Context) {
	status, err := tailscaletunnel.Start(c.Request.Context())
	if err != nil {
		resp.Error(c, tailscaleErrorStatus(err), err.Error())
		return
	}
	resp.Success(c, status)
}

func stopTailscaleFunnel(c *gin.Context) {
	status, err := tailscaletunnel.Stop(c.Request.Context())
	if err != nil {
		resp.Error(c, tailscaleErrorStatus(err), err.Error())
		return
	}
	resp.Success(c, status)
}

type tailscaleInstallRequest struct {
	SudoPassword string `json:"sudo_password"`
}

func installTailscale(c *gin.Context) {
	var req tailscaleInstallRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		resp.Error(c, http.StatusBadRequest, "invalid request body")
		return
	}

	status, err := tailscaletunnel.Install(c.Request.Context(), req.SudoPassword)
	if err != nil {
		resp.Error(c, tailscaleErrorStatus(err), err.Error())
		return
	}

	resp.Success(c, status)
}

func loginTailscale(c *gin.Context) {
	status, err := tailscaletunnel.Login(c.Request.Context())
	if err != nil {
		resp.Error(c, tailscaleErrorStatus(err), err.Error())
		return
	}

	resp.Success(c, status)
}

func tailscaleErrorStatus(err error) int {
	switch {
	case errors.Is(err, tailscaletunnel.ErrUnsupported),
		errors.Is(err, tailscaletunnel.ErrNotInstalled),
		errors.Is(err, tailscaletunnel.ErrNeedsLogin),
		errors.Is(err, tailscaletunnel.ErrDefaultCredential),
		errors.Is(err, tailscaletunnel.ErrSudoRequired),
		errors.Is(err, tailscaletunnel.ErrWindowsInstall),
		errors.Is(err, tailscaletunnel.ErrBrewMissing):
		return http.StatusBadRequest
	case errors.Is(err, tailscaletunnel.ErrConflict):
		return http.StatusConflict
	default:
		return http.StatusInternalServerError
	}
}
