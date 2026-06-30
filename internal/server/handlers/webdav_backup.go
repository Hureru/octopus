package handlers

import (
	"net/http"

	"github.com/bestruirui/octopus/internal/server/middleware"
	"github.com/bestruirui/octopus/internal/server/resp"
	"github.com/bestruirui/octopus/internal/server/router"
	"github.com/bestruirui/octopus/internal/webdav"
	"github.com/gin-gonic/gin"
)

func init() {
	router.NewGroupRouter("/api/v1/webdav-backup").
		Use(middleware.Auth()).
		AddRoute(
			router.NewRoute("/test", http.MethodPost).
				Handle(testWebDAVConnection),
		).
		AddRoute(
			router.NewRoute("/trigger", http.MethodPost).
				Handle(triggerWebDAVBackup),
		).
		AddRoute(
			router.NewRoute("/list", http.MethodGet).
				Handle(listWebDAVBackups),
		).
		AddRoute(
			router.NewRoute("/restore", http.MethodPost).
				Use(middleware.RequireJSON()).
				Handle(restoreWebDAVBackup),
		)
}

func testWebDAVConnection(c *gin.Context) {
	cfg, err := webdav.LoadConfig()
	if err != nil {
		resp.Error(c, http.StatusBadRequest, err.Error())
		return
	}

	if err := webdav.TestConnection(cfg); err != nil {
		resp.Error(c, http.StatusBadRequest, err.Error())
		return
	}

	resp.Success(c, gin.H{"ok": true})
}

func triggerWebDAVBackup(c *gin.Context) {
	if err := webdav.RunBackup(c.Request.Context()); err != nil {
		resp.Error(c, http.StatusInternalServerError, err.Error())
		return
	}

	resp.Success(c, gin.H{"message": "backup completed"})
}

func listWebDAVBackups(c *gin.Context) {
	backups, err := webdav.ListBackups(c.Request.Context())
	if err != nil {
		resp.Error(c, http.StatusInternalServerError, err.Error())
		return
	}

	resp.Success(c, backups)
}

func restoreWebDAVBackup(c *gin.Context) {
	var req struct {
		Filename string `json:"filename" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		resp.Error(c, http.StatusBadRequest, err.Error())
		return
	}

	result, err := webdav.RestoreFromBackup(c.Request.Context(), req.Filename)
	if err != nil {
		resp.Error(c, http.StatusInternalServerError, err.Error())
		return
	}

	resp.Success(c, result)
}
