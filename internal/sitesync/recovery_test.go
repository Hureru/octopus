package sitesync

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	dbpkg "github.com/bestruirui/octopus/internal/db"
	"github.com/bestruirui/octopus/internal/model"
	"github.com/bestruirui/octopus/internal/op"
	"gorm.io/gorm"
)

func TestRunSiteOperationWithRecoveryStopsOnCloudflare(t *testing.T) {
	ctx := setupProjectTestDB(t)
	siteRecord, account := createRecoveryFixture(t, ctx)
	disabled := false
	account.AutoProxyRecovery = &disabled
	if err := dbpkg.GetDB().WithContext(ctx).Model(&model.SiteAccount{}).
		Where("id = ?", account.ID).
		Update("auto_proxy_recovery", false).Error; err != nil {
		t.Fatalf("disable account recovery: %v", err)
	}

	calls := 0
	run := func(context.Context, *model.Site, *model.SiteAccount) (string, error) {
		calls++
		return "", wrapCloudflareProtectionError(newCloudflareProtectionError(403, nil))
	}
	for range 2 {
		if _, err := runSiteOperationWithRecovery(
			ctx,
			&siteRecord,
			&account,
			model.SiteOperationSync,
			run,
		); err == nil || !IsCloudflareProtectionError(err) {
			t.Fatalf("expected Cloudflare verification error, got %v", err)
		}
	}
	if calls != 2 {
		t.Fatalf("Cloudflare recovery should stop after one path per operation, got %d calls", calls)
	}

	var attempts []model.SiteOperationAttempt
	if err := dbpkg.GetDB().WithContext(ctx).Order("id ASC").Find(&attempts).Error; err != nil {
		t.Fatalf("load attempts: %v", err)
	}
	if len(attempts) != 2 {
		t.Fatalf("expected one attempt per operation, got %d", len(attempts))
	}
	for _, attempt := range attempts {
		if attempt.StopReason != "verification_required" || attempt.FailureClass != "cloudflare" {
			t.Fatalf("unexpected Cloudflare attempt: %+v", attempt)
		}
	}
	var sessionCount, taskCount int64
	if err := dbpkg.GetDB().WithContext(ctx).Model(&model.VerificationSession{}).
		Count(&sessionCount).Error; err != nil {
		t.Fatalf("count verification sessions: %v", err)
	}
	if err := dbpkg.GetDB().WithContext(ctx).Model(&model.VerificationTask{}).
		Count(&taskCount).Error; err != nil {
		t.Fatalf("count verification tasks: %v", err)
	}
	if sessionCount != 1 || taskCount != 1 {
		t.Fatalf("repeated Cloudflare stop created duplicate verification work: sessions=%d tasks=%d", sessionCount, taskCount)
	}
}

func TestRunSiteOperationWithRecoveryReportsVerificationPersistenceFailure(t *testing.T) {
	ctx := setupProjectTestDB(t)
	siteRecord, account := createRecoveryFixture(t, ctx)
	callbackName := "test:fail-verification-session-create"
	if err := dbpkg.GetDB().Callback().Create().Before("gorm:create").
		Register(callbackName, func(tx *gorm.DB) {
			if tx.Statement.Schema != nil &&
				tx.Statement.Schema.Name == "VerificationSession" {
				tx.AddError(errors.New("verification persistence unavailable"))
			}
		}); err != nil {
		t.Fatalf("register create callback: %v", err)
	}
	t.Cleanup(func() {
		_ = dbpkg.GetDB().Callback().Create().Remove(callbackName)
	})

	_, err := runSiteOperationWithRecovery(
		ctx,
		&siteRecord,
		&account,
		model.SiteOperationSync,
		func(context.Context, *model.Site, *model.SiteAccount) (string, error) {
			return "", wrapCloudflareProtectionError(newCloudflareProtectionError(403, nil))
		},
	)
	if err == nil ||
		!IsCloudflareProtectionError(err) ||
		!strings.Contains(err.Error(), "create verification session") {
		t.Fatalf("expected explicit verification persistence failure, got %v", err)
	}

	var attempts []model.SiteOperationAttempt
	if err := dbpkg.GetDB().WithContext(ctx).Find(&attempts).Error; err != nil {
		t.Fatalf("load attempts: %v", err)
	}
	if len(attempts) != 1 || attempts[0].StopReason != "verification_unavailable" {
		t.Fatalf("verification persistence failure was not audited: %+v", attempts)
	}
	var sessionCount int64
	if err := dbpkg.GetDB().WithContext(ctx).Model(&model.VerificationSession{}).
		Count(&sessionCount).Error; err != nil {
		t.Fatalf("count sessions: %v", err)
	}
	if sessionCount != 0 {
		t.Fatalf("failed verification persistence left %d sessions", sessionCount)
	}
}

func TestRunSiteOperationWithRecoveryDoesNotReplacePreferredPathWithDirectFallback(t *testing.T) {
	ctx := setupProjectTestDB(t)
	siteRecord, account := createRecoveryFixture(t, ctx)
	proxy := model.ProxyConfiguration{
		Name:    "failing-proxy",
		URL:     "http://127.0.0.1:18081",
		Enabled: true,
	}
	if err := op.ProxyConfigurationCreate(&proxy, ctx); err != nil {
		t.Fatalf("create proxy: %v", err)
	}
	siteRecord.PreferredProxyConfigID = &proxy.ID
	if err := dbpkg.GetDB().WithContext(ctx).Model(&model.Site{}).
		Where("id = ?", siteRecord.ID).
		Update("preferred_proxy_config_id", proxy.ID).Error; err != nil {
		t.Fatalf("set site preferred proxy: %v", err)
	}

	value, err := runSiteOperationWithRecovery(
		ctx,
		&siteRecord,
		&account,
		model.SiteOperationSync,
		func(_ context.Context, _ *model.Site, accountCopy *model.SiteAccount) (string, error) {
			if accountCopy.ProxyMode == model.ProxyUsageModePool {
				return "", &url.Error{Op: "GET", URL: "https://api.example.com", Err: errors.New("connection refused")}
			}
			return "direct-success", nil
		},
	)
	if err != nil {
		t.Fatalf("recover through direct path: %v", err)
	}
	if value != "direct-success" {
		t.Fatalf("unexpected recovery value: %q", value)
	}

	var reloaded model.Site
	if err := dbpkg.GetDB().WithContext(ctx).First(&reloaded, siteRecord.ID).Error; err != nil {
		t.Fatalf("reload site: %v", err)
	}
	if reloaded.PreferredProxyConfigID == nil || *reloaded.PreferredProxyConfigID != proxy.ID {
		t.Fatalf("direct fallback replaced the learned proxy preference: %+v", reloaded)
	}
}

func TestRunSiteOperationWithRecoveryUsesProxyEndpointWhenControllerIsUnavailable(t *testing.T) {
	ctx := setupProjectTestDB(t)
	siteRecord, account := createRecoveryFixture(t, ctx)
	controllerServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "controller offline", http.StatusServiceUnavailable)
	}))
	defer controllerServer.Close()

	controller := model.ClashController{
		Name:      "unavailable-controller",
		APIURL:    controllerServer.URL,
		ProxyURL:  "http://127.0.0.1:18082",
		GroupName: "Octopus",
		Enabled:   true,
	}
	if err := dbpkg.GetDB().WithContext(ctx).Create(&controller).Error; err != nil {
		t.Fatalf("create unavailable controller: %v", err)
	}
	proxy := model.ProxyConfiguration{
		Name:              "controller-backed-endpoint",
		URL:               controller.ProxyURL,
		ClashControllerID: &controller.ID,
		Enabled:           true,
	}
	if err := op.ProxyConfigurationCreate(&proxy, ctx); err != nil {
		t.Fatalf("create controller-backed proxy: %v", err)
	}
	siteRecord.PreferredProxyConfigID = &proxy.ID
	if err := dbpkg.GetDB().WithContext(ctx).Model(&model.Site{}).
		Where("id = ?", siteRecord.ID).
		Update("preferred_proxy_config_id", proxy.ID).Error; err != nil {
		t.Fatalf("set preferred proxy: %v", err)
	}

	value, err := runSiteOperationWithRecovery(
		ctx,
		&siteRecord,
		&account,
		model.SiteOperationSync,
		func(_ context.Context, _ *model.Site, accountCopy *model.SiteAccount) (string, error) {
			if accountCopy.ProxyMode != model.ProxyUsageModePool ||
				accountCopy.ProxyConfigID == nil ||
				*accountCopy.ProxyConfigID != proxy.ID {
				return "", errors.New("ordinary proxy endpoint was not attempted")
			}
			return "proxy-endpoint-success", nil
		},
	)
	if err != nil || value != "proxy-endpoint-success" {
		t.Fatalf("ordinary proxy endpoint did not survive controller outage: value=%q err=%v", value, err)
	}
	if proxyURL, err := op.ProxyURLForConfig(proxy.ID, ctx); err != nil || proxyURL != proxy.URL {
		t.Fatalf("proxy URL depends on controller availability: url=%q err=%v", proxyURL, err)
	}

	var attempts []model.SiteOperationAttempt
	if err := dbpkg.GetDB().WithContext(ctx).Find(&attempts).Error; err != nil {
		t.Fatalf("load recovery attempts: %v", err)
	}
	if len(attempts) != 1 ||
		!attempts[0].Success ||
		attempts[0].ProxyConfigID == nil ||
		*attempts[0].ProxyConfigID != proxy.ID ||
		attempts[0].ClashControllerID == nil ||
		*attempts[0].ClashControllerID != controller.ID ||
		!strings.Contains(attempts[0].Message, "controller unavailable") {
		t.Fatalf("controller outage fallback was not explicitly diagnosed: %+v", attempts)
	}
}

func TestBuildSiteRecoveryPathsKeepsConfiguredProxyBeforeDirect(t *testing.T) {
	ctx := setupProjectTestDB(t)
	siteRecord, account := createRecoveryFixture(t, ctx)
	proxy := model.ProxyConfiguration{
		Name:    "configured-proxy",
		URL:     "http://127.0.0.1:18083",
		Enabled: true,
	}
	if err := op.ProxyConfigurationCreate(&proxy, ctx); err != nil {
		t.Fatalf("create proxy: %v", err)
	}
	siteRecord.ProxyMode = model.ProxyUsageModePool
	siteRecord.ProxyConfigID = &proxy.ID

	paths, err := buildSiteRecoveryPaths(ctx, &siteRecord, &account)
	if err != nil {
		t.Fatalf("build recovery paths: %v", err)
	}
	if len(paths) < 2 ||
		paths[0].proxyMode != model.ProxyUsageModePool ||
		paths[0].proxyConfigID == nil ||
		*paths[0].proxyConfigID != proxy.ID ||
		paths[1].proxyMode != model.ProxyUsageModeDirect {
		t.Fatalf("configured proxy must precede direct fallback: %+v", paths)
	}

	if err := dbpkg.GetDB().WithContext(ctx).Model(&model.ProxyConfiguration{}).
		Where("id = ?", proxy.ID).
		Update("enabled", false).Error; err != nil {
		t.Fatalf("disable proxy: %v", err)
	}
	paths, err = buildSiteRecoveryPaths(ctx, &siteRecord, &account)
	if err != nil {
		t.Fatalf("build paths with disabled current proxy: %v", err)
	}
	if len(paths) == 0 || paths[0].proxyMode != model.ProxyUsageModeDirect {
		t.Fatalf("disabled current proxy should be skipped: %+v", paths)
	}
}

func TestBuildSiteRecoveryPathsKeepsExplicitPathAheadOfLearnedPreference(t *testing.T) {
	ctx := setupProjectTestDB(t)
	siteRecord, account := createRecoveryFixture(t, ctx)
	explicitProxy := model.ProxyConfiguration{
		Name: "explicit-proxy", URL: "http://127.0.0.1:18086", Enabled: true,
	}
	learnedProxy := model.ProxyConfiguration{
		Name: "learned-proxy", URL: "http://127.0.0.1:18087", Enabled: true,
	}
	if err := op.ProxyConfigurationCreate(&explicitProxy, ctx); err != nil {
		t.Fatalf("create explicit proxy: %v", err)
	}
	if err := op.ProxyConfigurationCreate(&learnedProxy, ctx); err != nil {
		t.Fatalf("create learned proxy: %v", err)
	}
	account.ProxyMode = model.ProxyUsageModePool
	account.ProxyConfigID = &explicitProxy.ID
	account.PreferredProxyConfigID = &learnedProxy.ID

	paths, err := buildSiteRecoveryPaths(ctx, &siteRecord, &account)
	if err != nil {
		t.Fatalf("build recovery paths: %v", err)
	}
	if len(paths) < 2 ||
		paths[0].proxyConfigID == nil || *paths[0].proxyConfigID != explicitProxy.ID ||
		paths[1].proxyConfigID == nil || *paths[1].proxyConfigID != learnedProxy.ID {
		t.Fatalf("explicit path must precede learned preference: %+v", paths)
	}
}

func TestLearnSiteRecoveryPathBootstrapsExplicitAccountOverride(t *testing.T) {
	ctx := setupProjectTestDB(t)
	siteRecord, account := createRecoveryFixture(t, ctx)
	proxy := model.ProxyConfiguration{
		Name: "account-bootstrap-proxy", URL: "http://127.0.0.1:18088", Enabled: true,
	}
	if err := op.ProxyConfigurationCreate(&proxy, ctx); err != nil {
		t.Fatalf("create proxy: %v", err)
	}
	account.ProxyMode = model.ProxyUsageModePool
	account.ProxyConfigID = &proxy.ID

	if err := learnSiteRecoveryPath(ctx, &siteRecord, &account, siteRecoveryPath{
		proxyMode:     model.ProxyUsageModePool,
		proxyConfigID: &proxy.ID,
	}); err != nil {
		t.Fatalf("learn account path: %v", err)
	}
	var reloaded model.SiteAccount
	if err := dbpkg.GetDB().WithContext(ctx).First(&reloaded, account.ID).Error; err != nil {
		t.Fatalf("reload account: %v", err)
	}
	if reloaded.PreferredProxyConfigID == nil || *reloaded.PreferredProxyConfigID != proxy.ID {
		t.Fatalf("account preference did not bootstrap: %+v", reloaded)
	}
}

func TestBuildSiteRecoveryPathsFallsBackFromUnavailableAccountPreferenceToSite(t *testing.T) {
	ctx := setupProjectTestDB(t)
	siteRecord, account := createRecoveryFixture(t, ctx)
	accountProxy := model.ProxyConfiguration{
		Name: "disabled-account-proxy", URL: "http://127.0.0.1:18084", Enabled: true,
	}
	siteProxy := model.ProxyConfiguration{
		Name: "healthy-site-proxy", URL: "http://127.0.0.1:18085", Enabled: true,
	}
	if err := op.ProxyConfigurationCreate(&accountProxy, ctx); err != nil {
		t.Fatalf("create account proxy: %v", err)
	}
	if err := op.ProxyConfigurationCreate(&siteProxy, ctx); err != nil {
		t.Fatalf("create site proxy: %v", err)
	}
	account.PreferredProxyConfigID = &accountProxy.ID
	siteRecord.PreferredProxyConfigID = &siteProxy.ID
	if err := dbpkg.GetDB().WithContext(ctx).Model(&model.ProxyConfiguration{}).
		Where("id = ?", accountProxy.ID).
		Update("enabled", false).Error; err != nil {
		t.Fatalf("disable account preferred proxy: %v", err)
	}

	paths, err := buildSiteRecoveryPaths(ctx, &siteRecord, &account)
	if err != nil {
		t.Fatalf("build recovery paths: %v", err)
	}
	if len(paths) == 0 ||
		paths[0].proxyConfigID == nil ||
		*paths[0].proxyConfigID != siteProxy.ID {
		t.Fatalf("site preference did not replace unavailable account override: %+v", paths)
	}
}

func TestRetryVerificationSessionRunsOriginalOperation(t *testing.T) {
	ctx := setupProjectTestDB(t)
	siteRecord, account := createRecoveryFixture(t, ctx)
	now := time.Now()
	session := model.VerificationSession{
		SiteID:        siteRecord.ID,
		SiteAccountID: account.ID,
		Status:        model.VerificationSessionCompleted,
		ExpiresAt:     now.Add(time.Hour),
		CompletedAt:   &now,
		Source:        "manual",
	}
	if err := dbpkg.GetDB().WithContext(ctx).Create(&session).Error; err != nil {
		t.Fatalf("create completed verification session: %v", err)
	}
	task := model.VerificationTask{
		SessionID:   session.ID,
		Status:      model.VerificationTaskCompleted,
		TargetURL:   siteRecord.BaseURL,
		TargetHost:  "api.example.com",
		ExpiresAt:   session.ExpiresAt,
		CompletedAt: &now,
		Operation:   model.SiteOperationSync,
		RetryStatus: model.VerificationRetryPending,
	}
	if err := dbpkg.GetDB().WithContext(ctx).Create(&task).Error; err != nil {
		t.Fatalf("create completed verification task: %v", err)
	}

	calls := 0
	err := retryVerificationSession(ctx, session.ID, verificationRetryRunner{
		syncAccount: func(_ context.Context, accountID int) (*model.SiteSyncResult, error) {
			calls++
			return &model.SiteSyncResult{
				AccountID: accountID,
				SiteID:    siteRecord.ID,
				Status:    model.SiteExecutionStatusSuccess,
				Message:   "sync restored",
			}, nil
		},
	})
	if err != nil {
		t.Fatalf("retry original operation: %v", err)
	}
	if calls != 1 {
		t.Fatalf("expected one original-operation retry, got %d", calls)
	}
	var reloaded model.VerificationTask
	if err := dbpkg.GetDB().WithContext(ctx).First(&reloaded, task.ID).Error; err != nil {
		t.Fatalf("reload retry task: %v", err)
	}
	if reloaded.RetryStatus != model.VerificationRetrySucceeded ||
		reloaded.RetryMessage != "sync restored" ||
		reloaded.RetryCompletedAt == nil {
		t.Fatalf("retry result was not persisted: %+v", reloaded)
	}
}

func createRecoveryFixture(t *testing.T, ctx context.Context) (model.Site, model.SiteAccount) {
	t.Helper()
	siteRecord := model.Site{
		Name:              "recovery-site",
		Platform:          model.SitePlatformNewAPI,
		BaseURL:           "https://api.example.com",
		Enabled:           true,
		ProxyMode:         model.ProxyUsageModeDirect,
		AutoProxyRecovery: true,
	}
	if err := dbpkg.GetDB().WithContext(ctx).Create(&siteRecord).Error; err != nil {
		t.Fatalf("create site: %v", err)
	}
	account := model.SiteAccount{
		SiteID:         siteRecord.ID,
		Name:           "recovery-account",
		CredentialType: model.SiteCredentialTypeAccessToken,
		AccessToken:    "test-token",
		ProxyMode:      model.ProxyUsageModeInherit,
		Enabled:        true,
	}
	if err := dbpkg.GetDB().WithContext(ctx).Create(&account).Error; err != nil {
		t.Fatalf("create account: %v", err)
	}
	return siteRecord, account
}
