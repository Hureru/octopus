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
	"github.com/bestruirui/octopus/internal/sitesync"
	"github.com/bestruirui/octopus/internal/utils/log"
	"github.com/bestruirui/octopus/internal/utils/safe"
	"github.com/gin-gonic/gin"
)

func init() {
	router.NewGroupRouter("/api/v1/proxy-pool/clash").
		Use(middleware.Auth()).
		AddRoute(router.NewRoute("/list", http.MethodGet).Handle(listClashControllers)).
		AddRoute(router.NewRoute("/:id/state", http.MethodGet).Handle(getClashControllerState)).
		AddRoute(router.NewRoute("/:id", http.MethodDelete).Handle(deleteClashController))
	router.NewGroupRouter("/api/v1/proxy-pool/clash").
		Use(middleware.Auth()).
		Use(middleware.RequireJSON()).
		AddRoute(router.NewRoute("/upsert", http.MethodPost).Handle(upsertClashController)).
		AddRoute(router.NewRoute("/:id/switch", http.MethodPost).Handle(switchClashControllerNode))

	router.NewGroupRouter("/api/v1/site/recovery").
		Use(middleware.Auth()).
		AddRoute(router.NewRoute("/attempts/:account_id", http.MethodGet).Handle(listSiteOperationAttempts)).
		AddRoute(router.NewRoute("/preferences/:account_id", http.MethodGet).Handle(listSiteProxyPreferences)).
		AddRoute(router.NewRoute("/preferences/account/:id", http.MethodDelete).Handle(clearAccountProxyPreference)).
		AddRoute(router.NewRoute("/preferences/site/:id", http.MethodDelete).Handle(clearSiteProxyPreference)).
		AddRoute(router.NewRoute("/verification", http.MethodGet).Handle(listVerificationSessions)).
		AddRoute(router.NewRoute("/verification/tasks", http.MethodGet).Handle(listVerificationTasks)).
		AddRoute(router.NewRoute("/verification/pairings", http.MethodGet).Handle(listVerificationPairings)).
		AddRoute(router.NewRoute("/verification/:id/retry", http.MethodPost).Handle(retryVerificationOperation)).
		AddRoute(router.NewRoute("/verification/:id", http.MethodDelete).Handle(revokeVerificationSession)).
		AddRoute(router.NewRoute("/verification/pairings/:id", http.MethodDelete).Handle(revokeVerificationPairing)).
		AddRoute(router.NewRoute("/verification/account/:id", http.MethodDelete).Handle(clearVerificationAccount))
	router.NewGroupRouter("/api/v1/site/recovery").
		Use(middleware.Auth()).
		Use(middleware.RequireJSON()).
		AddRoute(router.NewRoute("/verification", http.MethodPost).Handle(createVerificationSession)).
		AddRoute(router.NewRoute("/verification/:id/complete", http.MethodPost).Handle(manualCompleteVerificationSession)).
		AddRoute(router.NewRoute("/verification/pairings", http.MethodPost).Handle(createVerificationPairing))

	router.NewGroupRouter("/api/v1/site/recovery/verification/bridge").
		Use(middleware.RequireJSON()).
		AddRoute(router.NewRoute("/claim", http.MethodPost).Handle(claimVerificationTask)).
		AddRoute(router.NewRoute("/release", http.MethodPost).Handle(releaseVerificationTask)).
		AddRoute(router.NewRoute("/complete", http.MethodPost).Handle(completeVerificationTask))
}

func listClashControllers(c *gin.Context) {
	items, err := op.ClashControllerList(c.Request.Context())
	if err != nil {
		resp.Error(c, http.StatusInternalServerError, err.Error())
		return
	}
	resp.Success(c, items)
}

func upsertClashController(c *gin.Context) {
	var request op.ClashControllerInput
	if err := c.ShouldBindJSON(&request); err != nil {
		resp.InvalidJSON(c)
		return
	}
	item, err := op.ClashControllerUpsert(c.Request.Context(), request)
	if err != nil {
		resp.Error(c, http.StatusBadRequest, err.Error())
		return
	}
	resp.Success(c, item)
}

func deleteClashController(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil || id <= 0 {
		resp.InvalidParam(c)
		return
	}
	if err := op.ClashControllerDelete(c.Request.Context(), id); err != nil {
		resp.Error(c, http.StatusBadRequest, err.Error())
		return
	}
	resp.Success(c, nil)
}

func getClashControllerState(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil || id <= 0 {
		resp.InvalidParam(c)
		return
	}
	item, err := op.ClashControllerState(c.Request.Context(), id)
	if err != nil {
		resp.Error(c, http.StatusBadRequest, err.Error())
		return
	}
	resp.Success(c, item)
}

func switchClashControllerNode(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil || id <= 0 {
		resp.InvalidParam(c)
		return
	}
	var request struct {
		Node string `json:"node" binding:"required"`
	}
	if err := c.ShouldBindJSON(&request); err != nil {
		resp.InvalidJSON(c)
		return
	}
	if err := op.ClashSwitchNode(c.Request.Context(), id, request.Node); err != nil {
		resp.Error(c, http.StatusBadRequest, err.Error())
		return
	}
	resp.Success(c, nil)
}

func listSiteOperationAttempts(c *gin.Context) {
	accountID, err := strconv.Atoi(c.Param("account_id"))
	if err != nil || accountID <= 0 {
		resp.InvalidParam(c)
		return
	}
	limit, _ := strconv.Atoi(c.Query("limit"))
	items, err := sitesync.SiteOperationAttemptList(c.Request.Context(), accountID, limit)
	if err != nil {
		resp.Error(c, http.StatusInternalServerError, err.Error())
		return
	}
	resp.Success(c, items)
}

func listSiteProxyPreferences(c *gin.Context) {
	accountID, err := strconv.Atoi(c.Param("account_id"))
	if err != nil || accountID <= 0 {
		resp.InvalidParam(c)
		return
	}
	account, err := op.SiteAccountGet(accountID, c.Request.Context())
	if err != nil {
		resp.Error(c, http.StatusNotFound, "site account not found")
		return
	}
	items, err := op.SiteProxyPreferenceList(c.Request.Context(), account.SiteID, account.ID)
	if err != nil {
		resp.Error(c, http.StatusInternalServerError, err.Error())
		return
	}
	resp.Success(c, items)
}

func clearAccountProxyPreference(c *gin.Context) {
	accountID, err := strconv.Atoi(c.Param("id"))
	if err != nil || accountID <= 0 {
		resp.InvalidParam(c)
		return
	}
	if err := op.SiteProxyPreferenceClearAccount(c.Request.Context(), accountID); err != nil {
		resp.Error(c, http.StatusBadRequest, err.Error())
		return
	}
	resp.Success(c, nil)
}

func clearSiteProxyPreference(c *gin.Context) {
	siteID, err := strconv.Atoi(c.Param("id"))
	if err != nil || siteID <= 0 {
		resp.InvalidParam(c)
		return
	}
	if err := op.SiteProxyPreferenceClearSite(c.Request.Context(), siteID); err != nil {
		resp.Error(c, http.StatusBadRequest, err.Error())
		return
	}
	resp.Success(c, nil)
}

func createVerificationSession(c *gin.Context) {
	var request op.VerificationSessionCreateRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		resp.InvalidJSON(c)
		return
	}
	item, err := op.VerificationSessionCreate(c.Request.Context(), request)
	if err != nil {
		resp.Error(c, http.StatusBadRequest, err.Error())
		return
	}
	resp.Success(c, item)
}

func manualCompleteVerificationSession(c *gin.Context) {
	sessionID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || sessionID <= 0 {
		resp.InvalidParam(c)
		return
	}
	var request struct {
		Cookie    string `json:"cookie" binding:"required"`
		UserAgent string `json:"user_agent,omitempty"`
	}
	if err := c.ShouldBindJSON(&request); err != nil {
		resp.InvalidJSON(c)
		return
	}
	item, err := op.VerificationSessionManualComplete(
		c.Request.Context(),
		sessionID,
		request.Cookie,
		request.UserAgent,
	)
	if err != nil {
		resp.Error(c, http.StatusBadRequest, err.Error())
		return
	}
	scheduleVerificationRetry(item.ID)
	resp.Success(c, item)
}

func createVerificationPairing(c *gin.Context) {
	var request struct {
		Name    string `json:"name" binding:"required"`
		TTLDays int    `json:"ttl_days,omitempty"`
	}
	if err := c.ShouldBindJSON(&request); err != nil {
		resp.InvalidJSON(c)
		return
	}
	item, err := op.VerificationBridgePairingCreate(c.Request.Context(), request.Name, request.TTLDays)
	if err != nil {
		resp.Error(c, http.StatusBadRequest, err.Error())
		return
	}
	resp.Success(c, item)
}

func listVerificationPairings(c *gin.Context) {
	items, err := op.VerificationBridgePairingList(c.Request.Context())
	if err != nil {
		resp.Error(c, http.StatusInternalServerError, err.Error())
		return
	}
	resp.Success(c, items)
}

func revokeVerificationPairing(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || id <= 0 {
		resp.InvalidParam(c)
		return
	}
	if err := op.VerificationBridgePairingRevoke(c.Request.Context(), id); err != nil {
		resp.Error(c, http.StatusBadRequest, err.Error())
		return
	}
	resp.Success(c, nil)
}

func listVerificationTasks(c *gin.Context) {
	accountID, ok := optionalPositiveIntQuery(c, "account_id")
	if !ok {
		resp.InvalidParam(c)
		return
	}
	items, err := op.VerificationTaskList(c.Request.Context(), accountID)
	if err != nil {
		resp.Error(c, http.StatusInternalServerError, err.Error())
		return
	}
	resp.Success(c, items)
}

func claimVerificationTask(c *gin.Context) {
	var request struct {
		PairingToken string `json:"pairing_token" binding:"required"`
	}
	if err := c.ShouldBindJSON(&request); err != nil {
		resp.InvalidJSON(c)
		return
	}
	item, err := op.VerificationTaskClaim(c.Request.Context(), request.PairingToken)
	if err != nil {
		resp.Error(c, http.StatusBadRequest, err.Error())
		return
	}
	resp.Success(c, item)
}

func completeVerificationTask(c *gin.Context) {
	var request struct {
		PairingToken string                       `json:"pairing_token" binding:"required"`
		TaskToken    string                       `json:"task_token" binding:"required"`
		Cookies      []op.VerificationCookieInput `json:"cookies" binding:"required"`
		UserAgent    string                       `json:"user_agent,omitempty"`
	}
	if err := c.ShouldBindJSON(&request); err != nil {
		resp.InvalidJSON(c)
		return
	}
	item, err := op.VerificationTaskComplete(
		c.Request.Context(),
		request.PairingToken,
		request.TaskToken,
		request.Cookies,
		request.UserAgent,
	)
	if err != nil {
		resp.Error(c, http.StatusBadRequest, err.Error())
		return
	}
	scheduleVerificationRetry(item.ID)
	resp.Success(c, item)
}

func releaseVerificationTask(c *gin.Context) {
	var request struct {
		PairingToken string `json:"pairing_token" binding:"required"`
		TaskToken    string `json:"task_token" binding:"required"`
	}
	if err := c.ShouldBindJSON(&request); err != nil {
		resp.InvalidJSON(c)
		return
	}
	if err := op.VerificationTaskRelease(
		c.Request.Context(),
		request.PairingToken,
		request.TaskToken,
	); err != nil {
		resp.Error(c, http.StatusBadRequest, err.Error())
		return
	}
	resp.Success(c, nil)
}

func listVerificationSessions(c *gin.Context) {
	accountID, ok := optionalPositiveIntQuery(c, "account_id")
	if !ok {
		resp.InvalidParam(c)
		return
	}
	items, err := op.VerificationSessionList(c.Request.Context(), accountID)
	if err != nil {
		resp.Error(c, http.StatusInternalServerError, err.Error())
		return
	}
	resp.Success(c, items)
}

func revokeVerificationSession(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || id <= 0 {
		resp.InvalidParam(c)
		return
	}
	if err := op.VerificationSessionRevoke(c.Request.Context(), id); err != nil {
		resp.Error(c, http.StatusBadRequest, err.Error())
		return
	}
	resp.Success(c, nil)
}

func clearVerificationAccount(c *gin.Context) {
	accountID, err := strconv.Atoi(c.Param("id"))
	if err != nil || accountID <= 0 {
		resp.InvalidParam(c)
		return
	}
	if err := op.VerificationSessionClearAccount(c.Request.Context(), accountID); err != nil {
		resp.Error(c, http.StatusBadRequest, err.Error())
		return
	}
	resp.Success(c, model.SiteExecutionStatusSuccess)
}

func retryVerificationOperation(c *gin.Context) {
	retryVerificationOperationWith(c, op.VerificationRetryRequeue, scheduleVerificationRetry)
}

func retryVerificationOperationWith(
	c *gin.Context,
	requeue func(context.Context, int64) error,
	schedule func(int64),
) {
	sessionID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || sessionID <= 0 {
		resp.InvalidParam(c)
		return
	}
	if err := requeue(c.Request.Context(), sessionID); err != nil {
		resp.Error(c, http.StatusBadRequest, err.Error())
		return
	}
	schedule(sessionID)
	resp.Success(c, model.VerificationRetryPending)
}

func scheduleVerificationRetry(sessionID int64) {
	if sessionID <= 0 {
		return
	}
	safe.Go("site-verification-retry", func() {
		ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
		defer cancel()
		if err := sitesync.RetryVerificationSession(ctx, sessionID); err != nil {
			log.Warnw(
				"site verification retry failed",
				"session_id", sessionID,
				"error", err,
			)
		}
	})
}

func optionalPositiveIntQuery(c *gin.Context, name string) (int, bool) {
	raw, exists := c.GetQuery(name)
	if !exists {
		return 0, true
	}
	value, err := strconv.Atoi(raw)
	if err != nil || value <= 0 {
		return 0, false
	}
	return value, true
}
