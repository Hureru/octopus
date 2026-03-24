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
		AddRoute(router.NewRoute("/account/sync/:id", http.MethodPost).Handle(syncSiteAccount)).
		AddRoute(router.NewRoute("/account/checkin/:id", http.MethodPost).Handle(checkinSiteAccount)).
		AddRoute(router.NewRoute("/sync-all", http.MethodPost).Handle(syncAllSiteAccounts)).
		AddRoute(router.NewRoute("/checkin-all", http.MethodPost).Handle(checkinAllSiteAccounts))

	router.NewGroupRouter("/api/v1/site").
		Use(middleware.Auth()).
		Use(middleware.RequireJSON()).
		AddRoute(router.NewRoute("/create", http.MethodPost).Handle(createSite)).
		AddRoute(router.NewRoute("/update", http.MethodPost).Handle(updateSite)).
		AddRoute(router.NewRoute("/enable", http.MethodPost).Handle(enableSite)).
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
