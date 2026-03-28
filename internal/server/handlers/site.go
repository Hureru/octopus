package handlers

import (
	"context"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/bestruirui/octopus/internal/model"
	"github.com/bestruirui/octopus/internal/op"
	"github.com/bestruirui/octopus/internal/server/middleware"
	"github.com/bestruirui/octopus/internal/server/resp"
	"github.com/bestruirui/octopus/internal/server/router"
	sitesvc "github.com/bestruirui/octopus/internal/site"
	"github.com/bestruirui/octopus/internal/utils/log"
	"github.com/gin-gonic/gin"
)

func refreshAccountRandomCheckinScheduleBestEffort(ctx context.Context, accountID int) {
	if err := sitesvc.RefreshAccountRandomCheckinSchedule(ctx, accountID); err != nil {
		log.Warnf("failed to refresh random checkin schedule (account=%d): %v", accountID, err)
	}
}

func init() {
	router.NewGroupRouter("/api/v1/site").
		Use(middleware.Auth()).
		AddRoute(router.NewRoute("/list", http.MethodGet).Handle(listSite)).
		AddRoute(router.NewRoute("/import/all-api-hub", http.MethodPost).Handle(importAllAPIHub)).
		AddRoute(router.NewRoute("/account/sync/:id", http.MethodPost).Handle(syncSiteAccount)).
		AddRoute(router.NewRoute("/account/checkin/:id", http.MethodPost).Handle(checkinSiteAccount)).
		AddRoute(router.NewRoute("/sync-all", http.MethodPost).Handle(syncAllSiteAccounts)).
		AddRoute(router.NewRoute("/checkin-all", http.MethodPost).Handle(checkinAllSiteAccounts)).
		AddRoute(router.NewRoute("/:id/available-models", http.MethodGet).Handle(getSiteAvailableModels))

	router.NewGroupRouter("/api/v1/site").
		Use(middleware.Auth()).
		Use(middleware.RequireJSON()).
		AddRoute(router.NewRoute("/create", http.MethodPost).Handle(createSite)).
		AddRoute(router.NewRoute("/update", http.MethodPost).Handle(updateSite)).
		AddRoute(router.NewRoute("/enable", http.MethodPost).Handle(enableSite)).
		AddRoute(router.NewRoute("/detect", http.MethodPost).Handle(detectSitePlatform)).
		AddRoute(router.NewRoute("/batch", http.MethodPost).Handle(batchSite)).
		AddRoute(router.NewRoute("/account/create", http.MethodPost).Handle(createSiteAccount)).
		AddRoute(router.NewRoute("/account/update", http.MethodPost).Handle(updateSiteAccount)).
		AddRoute(router.NewRoute("/account/enable", http.MethodPost).Handle(enableSiteAccount))

	router.NewGroupRouter("/api/v1/site").
		Use(middleware.Auth()).
		AddRoute(router.NewRoute("/delete/:id", http.MethodDelete).Handle(deleteSite)).
		AddRoute(router.NewRoute("/account/delete/:id", http.MethodDelete).Handle(deleteSiteAccount))
}

func listSite(c *gin.Context) {
	sites, err := op.SiteList(c.Request.Context())
	if err != nil {
		resp.Error(c, http.StatusInternalServerError, err.Error())
		return
	}
	resp.Success(c, sites)
}

func importAllAPIHub(c *gin.Context) {
	body, err := readImportPayload(c)
	if err != nil {
		resp.Error(c, http.StatusBadRequest, err.Error())
		return
	}

	result, syncAccountIDs, err := op.SiteImportAllAPIHub(c.Request.Context(), body)
	if err != nil {
		resp.Error(c, http.StatusBadRequest, err.Error())
		return
	}

	for _, accountID := range syncAccountIDs {
		go func(accountID int) {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
			defer cancel()
			_, syncErr := sitesvc.SyncAccount(ctx, accountID)
			if syncErr != nil {
				log.Warnf("failed to sync imported all api hub account %d: %v", accountID, syncErr)
			}
		}(accountID)
	}

	resp.Success(c, result)
}

func readImportPayload(c *gin.Context) ([]byte, error) {
	contentType := c.GetHeader("Content-Type")
	if strings.Contains(contentType, "multipart/form-data") {
		fileHeader, err := c.FormFile("file")
		if err != nil {
			return nil, err
		}
		file, err := fileHeader.Open()
		if err != nil {
			return nil, err
		}
		defer file.Close()
		return io.ReadAll(file)
	}
	return io.ReadAll(c.Request.Body)
}

func createSite(c *gin.Context) {
	var site model.Site
	if err := c.ShouldBindJSON(&site); err != nil {
		resp.Error(c, http.StatusBadRequest, resp.ErrInvalidJSON)
		return
	}
	if err := site.Validate(); err != nil {
		resp.Error(c, http.StatusBadRequest, err.Error())
		return
	}
	if err := op.SiteCreate(&site, c.Request.Context()); err != nil {
		resp.Error(c, http.StatusInternalServerError, err.Error())
		return
	}
	resp.Success(c, site)
}

func updateSite(c *gin.Context) {
	var req model.SiteUpdateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		resp.Error(c, http.StatusBadRequest, resp.ErrInvalidJSON)
		return
	}
	site, err := op.SiteUpdate(&req, c.Request.Context())
	if err != nil {
		resp.Error(c, http.StatusInternalServerError, err.Error())
		return
	}
	go func(siteID int) {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
		defer cancel()
		_ = sitesvc.ProjectSite(ctx, siteID)
	}(site.ID)
	resp.Success(c, site)
}

func enableSite(c *gin.Context) {
	var request struct {
		ID      int  `json:"id"`
		Enabled bool `json:"enabled"`
	}
	if err := c.ShouldBindJSON(&request); err != nil {
		resp.Error(c, http.StatusBadRequest, resp.ErrInvalidJSON)
		return
	}
	if err := op.SiteEnabled(request.ID, request.Enabled, c.Request.Context()); err != nil {
		resp.Error(c, http.StatusInternalServerError, err.Error())
		return
	}
	go func(siteID int) {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
		defer cancel()
		_ = sitesvc.ProjectSite(ctx, siteID)
	}(request.ID)
	resp.Success(c, nil)
}

func deleteSite(c *gin.Context) {
	idNum, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		resp.Error(c, http.StatusBadRequest, resp.ErrInvalidParam)
		return
	}
	if err := sitesvc.DeleteSite(c.Request.Context(), idNum); err != nil {
		resp.Error(c, http.StatusInternalServerError, err.Error())
		return
	}
	resp.Success(c, nil)
}

func createSiteAccount(c *gin.Context) {
	var account model.SiteAccount
	if err := c.ShouldBindJSON(&account); err != nil {
		resp.Error(c, http.StatusBadRequest, resp.ErrInvalidJSON)
		return
	}
	if err := account.Validate(); err != nil {
		resp.Error(c, http.StatusBadRequest, err.Error())
		return
	}
	if err := op.SiteAccountCreate(&account, c.Request.Context()); err != nil {
		resp.Error(c, http.StatusInternalServerError, err.Error())
		return
	}
	refreshAccountRandomCheckinScheduleBestEffort(c.Request.Context(), account.ID)
	createdAccount, err := op.SiteAccountGet(account.ID, c.Request.Context())
	if err != nil {
		resp.Error(c, http.StatusInternalServerError, err.Error())
		return
	}
	if account.Enabled && account.AutoSync {
		go func(accountID int) {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
			defer cancel()
			_, _ = sitesvc.SyncAccount(ctx, accountID)
		}(account.ID)
	}
	resp.Success(c, createdAccount)
}

func updateSiteAccount(c *gin.Context) {
	var req model.SiteAccountUpdateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		resp.Error(c, http.StatusBadRequest, resp.ErrInvalidJSON)
		return
	}
	account, err := op.SiteAccountUpdate(&req, c.Request.Context())
	if err != nil {
		resp.Error(c, http.StatusInternalServerError, err.Error())
		return
	}
	refreshAccountRandomCheckinScheduleBestEffort(c.Request.Context(), account.ID)
	account, err = op.SiteAccountGet(account.ID, c.Request.Context())
	if err != nil {
		resp.Error(c, http.StatusInternalServerError, err.Error())
		return
	}
	go func(accountID int, autoSync bool) {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
		defer cancel()
		_, _ = sitesvc.ProjectAccount(ctx, accountID)
		if autoSync {
			_, _ = sitesvc.SyncAccount(ctx, accountID)
		}
	}(account.ID, account.AutoSync)
	resp.Success(c, account)
}

func enableSiteAccount(c *gin.Context) {
	var request struct {
		ID      int  `json:"id"`
		Enabled bool `json:"enabled"`
	}
	if err := c.ShouldBindJSON(&request); err != nil {
		resp.Error(c, http.StatusBadRequest, resp.ErrInvalidJSON)
		return
	}
	if err := op.SiteAccountEnabled(request.ID, request.Enabled, c.Request.Context()); err != nil {
		resp.Error(c, http.StatusInternalServerError, err.Error())
		return
	}
	refreshAccountRandomCheckinScheduleBestEffort(c.Request.Context(), request.ID)
	go func(accountID int) {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
		defer cancel()
		_, _ = sitesvc.ProjectAccount(ctx, accountID)
	}(request.ID)
	resp.Success(c, nil)
}

func deleteSiteAccount(c *gin.Context) {
	idNum, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		resp.Error(c, http.StatusBadRequest, resp.ErrInvalidParam)
		return
	}
	if err := sitesvc.DeleteSiteAccount(c.Request.Context(), idNum); err != nil {
		resp.Error(c, http.StatusInternalServerError, err.Error())
		return
	}
	resp.Success(c, nil)
}

func syncSiteAccount(c *gin.Context) {
	idNum, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		resp.Error(c, http.StatusBadRequest, resp.ErrInvalidParam)
		return
	}
	result, err := sitesvc.SyncAccount(c.Request.Context(), idNum)
	if err != nil {
		resp.Error(c, http.StatusInternalServerError, err.Error())
		return
	}
	resp.Success(c, result)
}

func checkinSiteAccount(c *gin.Context) {
	idNum, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		resp.Error(c, http.StatusBadRequest, resp.ErrInvalidParam)
		return
	}
	result, err := sitesvc.CheckinAccount(c.Request.Context(), idNum)
	if err != nil {
		resp.Error(c, http.StatusInternalServerError, err.Error())
		return
	}
	resp.Success(c, result)
}

func syncAllSiteAccounts(c *gin.Context) {
	go sitesvc.SyncAll(context.Background())
	resp.Success(c, nil)
}

func checkinAllSiteAccounts(c *gin.Context) {
	go sitesvc.CheckinAll(context.Background())
	resp.Success(c, nil)
}

func detectSitePlatform(c *gin.Context) {
	var request struct {
		URL string `json:"url" binding:"required"`
	}
	if err := c.ShouldBindJSON(&request); err != nil {
		resp.Error(c, http.StatusBadRequest, resp.ErrInvalidJSON)
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 30*time.Second)
	defer cancel()
	platform, err := sitesvc.DetectPlatform(ctx, request.URL)
	if err != nil {
		resp.Error(c, http.StatusBadRequest, err.Error())
		return
	}
	resp.Success(c, gin.H{"platform": platform})
}

func batchSite(c *gin.Context) {
	var req model.SiteBatchRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		resp.Error(c, http.StatusBadRequest, resp.ErrInvalidJSON)
		return
	}
	validActions := map[string]bool{
		"enable": true, "disable": true, "delete": true,
		"enable_system_proxy": true, "disable_system_proxy": true,
	}
	if !validActions[req.Action] {
		resp.Error(c, http.StatusBadRequest, "invalid action")
		return
	}
	if len(req.IDs) == 0 {
		resp.Error(c, http.StatusBadRequest, "ids is required")
		return
	}

	result := model.SiteBatchResult{
		SuccessIDs:  make([]int, 0),
		FailedItems: make([]model.SiteBatchFailure, 0),
	}
	ctx := c.Request.Context()

	for _, id := range req.IDs {
		var batchErr error
		switch req.Action {
		case "enable":
			batchErr = op.SiteEnabled(id, true, ctx)
		case "disable":
			batchErr = op.SiteEnabled(id, false, ctx)
		case "delete":
			batchErr = sitesvc.DeleteSite(ctx, id)
		case "enable_system_proxy":
			batchErr = op.SiteUpdateSystemProxy(id, true, ctx)
		case "disable_system_proxy":
			batchErr = op.SiteUpdateSystemProxy(id, false, ctx)
		}
		if batchErr != nil {
			result.FailedItems = append(result.FailedItems, model.SiteBatchFailure{ID: id, Message: batchErr.Error()})
		} else {
			result.SuccessIDs = append(result.SuccessIDs, id)
		}
	}

	// Project affected sites asynchronously
	if req.Action == "enable" || req.Action == "disable" {
		for _, id := range result.SuccessIDs {
			go func(siteID int) {
				projCtx, projCancel := context.WithTimeout(context.Background(), 5*time.Minute)
				defer projCancel()
				_ = sitesvc.ProjectSite(projCtx, siteID)
			}(id)
		}
	}

	resp.Success(c, result)
}

func getSiteAvailableModels(c *gin.Context) {
	idNum, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		resp.Error(c, http.StatusBadRequest, resp.ErrInvalidParam)
		return
	}
	models, err := op.SiteAvailableModels(idNum, c.Request.Context())
	if err != nil {
		resp.Error(c, http.StatusInternalServerError, err.Error())
		return
	}
	resp.Success(c, gin.H{"site_id": idNum, "models": models})
}
