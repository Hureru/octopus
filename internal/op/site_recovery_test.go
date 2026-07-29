package op

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	dbpkg "github.com/bestruirui/octopus/internal/db"
	"github.com/bestruirui/octopus/internal/model"
)

func TestVerificationSessionEnsureKeepsExplicitDirectBinding(t *testing.T) {
	ctx := setupBackupTestDB(t)
	_, account := createVerificationFixture(t, ctx)
	proxy := model.ProxyConfiguration{
		Name: "preferred-proxy",
		URL:  "http://127.0.0.1:18080",
	}
	if err := ProxyConfigurationCreate(&proxy, ctx); err != nil {
		t.Fatalf("create proxy: %v", err)
	}
	if err := dbpkg.GetDB().WithContext(ctx).Model(&account).
		Update("preferred_proxy_config_id", proxy.ID).Error; err != nil {
		t.Fatalf("set account preference: %v", err)
	}

	first, err := VerificationSessionEnsure(ctx, VerificationSessionCreateRequest{
		SiteAccountID: account.ID,
		ProxyConfigID: nil,
		ClashNode:     "",
	})
	if err != nil {
		t.Fatalf("ensure direct verification session: %v", err)
	}
	second, err := VerificationSessionEnsure(ctx, VerificationSessionCreateRequest{
		SiteAccountID: account.ID,
		ProxyConfigID: nil,
		ClashNode:     "",
	})
	if err != nil {
		t.Fatalf("reuse direct verification session: %v", err)
	}

	if first.Session.ProxyConfigID != nil {
		t.Fatalf("direct verification unexpectedly inherited proxy %v", *first.Session.ProxyConfigID)
	}
	if second.Session.ID != first.Session.ID || second.Task.ID != first.Task.ID {
		t.Fatalf("ensure created duplicate direct verification work: first=%+v second=%+v", first, second)
	}
}

func TestVerificationSessionSourceIsSetByCompletionMethod(t *testing.T) {
	ctx := setupBackupTestDB(t)
	_, account := createVerificationFixture(t, ctx)
	created, err := VerificationSessionCreate(ctx, VerificationSessionCreateRequest{
		SiteAccountID: account.ID,
	})
	if err != nil {
		t.Fatalf("create verification session: %v", err)
	}
	if created.Session.Source != "" {
		t.Fatalf("pending verification session should not claim a source: %+v", created.Session)
	}

	completed, err := VerificationSessionManualComplete(
		ctx,
		created.Session.ID,
		"cf_clearance=value",
		"",
	)
	if err != nil {
		t.Fatalf("complete verification session: %v", err)
	}
	if completed.Source != "manual" {
		t.Fatalf("unexpected verification completion source: %+v", completed)
	}
}

func TestVerificationTaskClaimIsSingleConsumer(t *testing.T) {
	ctx := setupBackupTestDB(t)
	_, account := createVerificationFixture(t, ctx)
	if _, err := VerificationSessionCreate(ctx, VerificationSessionCreateRequest{
		SiteAccountID: account.ID,
	}); err != nil {
		t.Fatalf("create verification session: %v", err)
	}
	pairing, err := VerificationBridgePairingCreate(ctx, "test bridge", 1)
	if err != nil {
		t.Fatalf("create pairing: %v", err)
	}

	start := make(chan struct{})
	results := make(chan error, 2)
	var wait sync.WaitGroup
	for range 2 {
		wait.Add(1)
		go func() {
			defer wait.Done()
			<-start
			_, claimErr := VerificationTaskClaim(ctx, pairing.Token)
			results <- claimErr
		}()
	}
	close(start)
	wait.Wait()
	close(results)

	successes := 0
	for claimErr := range results {
		if claimErr == nil {
			successes++
		}
	}
	if successes != 1 {
		t.Fatalf("expected exactly one successful claim, got %d", successes)
	}
}

func TestVerificationTaskCompleteRejectsPublicSuffixAndCrossDomainCookies(t *testing.T) {
	for _, test := range []struct {
		name   string
		domain string
	}{
		{name: "public suffix", domain: "com"},
		{name: "cross domain", domain: "example.net"},
	} {
		t.Run(test.name, func(t *testing.T) {
			_, err := verificationCookieHeader("api.example.com", []VerificationCookieInput{
				{Name: "cf_clearance", Value: "value", Domain: test.domain},
			})
			if err == nil {
				t.Fatalf("cookie domain %q should be rejected", test.domain)
			}
		})
	}

	header, err := verificationCookieHeader("api.example.com", []VerificationCookieInput{
		{Name: "cf_clearance", Value: "value", Domain: ".example.com"},
	})
	if err != nil {
		t.Fatalf("valid parent-domain cookie rejected: %v", err)
	}
	if header != "cf_clearance=value" {
		t.Fatalf("unexpected cookie header: %q", header)
	}
}

func TestVerificationPairingRevokeRequeuesClaimedTask(t *testing.T) {
	ctx := setupBackupTestDB(t)
	_, account := createVerificationFixture(t, ctx)
	created, err := VerificationSessionCreate(ctx, VerificationSessionCreateRequest{
		SiteAccountID: account.ID,
	})
	if err != nil {
		t.Fatalf("create verification session: %v", err)
	}
	firstPairing, err := VerificationBridgePairingCreate(ctx, "first bridge", 1)
	if err != nil {
		t.Fatalf("create first pairing: %v", err)
	}
	if _, err := VerificationTaskClaim(ctx, firstPairing.Token); err != nil {
		t.Fatalf("claim task: %v", err)
	}

	if err := VerificationBridgePairingRevoke(ctx, firstPairing.Pairing.ID); err != nil {
		t.Fatalf("revoke pairing: %v", err)
	}
	var released model.VerificationTask
	if err := dbpkg.GetDB().WithContext(ctx).First(&released, created.Task.ID).Error; err != nil {
		t.Fatalf("reload released task: %v", err)
	}
	if released.Status != model.VerificationTaskPending ||
		released.PairingID != nil ||
		released.ClaimTokenHash != "" ||
		released.ClaimedAt != nil {
		t.Fatalf("revoked pairing did not release task for retry: %+v", released)
	}

	secondPairing, err := VerificationBridgePairingCreate(ctx, "second bridge", 1)
	if err != nil {
		t.Fatalf("create second pairing: %v", err)
	}
	reclaimed, err := VerificationTaskClaim(ctx, secondPairing.Token)
	if err != nil {
		t.Fatalf("reclaim task: %v", err)
	}
	if reclaimed.Task.ID != created.Task.ID {
		t.Fatalf("reclaimed wrong task: got %d want %d", reclaimed.Task.ID, created.Task.ID)
	}
}

func TestVerificationTaskClaimReleasesStaleClaim(t *testing.T) {
	ctx := setupBackupTestDB(t)
	_, account := createVerificationFixture(t, ctx)
	created, err := VerificationSessionCreate(ctx, VerificationSessionCreateRequest{
		SiteAccountID: account.ID,
	})
	if err != nil {
		t.Fatalf("create verification session: %v", err)
	}
	firstPairing, err := VerificationBridgePairingCreate(ctx, "stale bridge", 1)
	if err != nil {
		t.Fatalf("create first pairing: %v", err)
	}
	firstClaim, err := VerificationTaskClaim(ctx, firstPairing.Token)
	if err != nil {
		t.Fatalf("claim task: %v", err)
	}
	staleAt := time.Now().Add(-verificationTaskClaimTTL - time.Second)
	if err := dbpkg.GetDB().WithContext(ctx).Model(&model.VerificationTask{}).
		Where("id = ?", created.Task.ID).
		Update("claimed_at", staleAt).Error; err != nil {
		t.Fatalf("age claimed task: %v", err)
	}

	secondPairing, err := VerificationBridgePairingCreate(ctx, "replacement bridge", 1)
	if err != nil {
		t.Fatalf("create second pairing: %v", err)
	}
	secondClaim, err := VerificationTaskClaim(ctx, secondPairing.Token)
	if err != nil {
		t.Fatalf("reclaim stale task: %v", err)
	}
	if secondClaim.Task.ID != created.Task.ID || secondClaim.TaskToken == firstClaim.TaskToken {
		t.Fatalf("stale task was not safely reissued: first=%+v second=%+v", firstClaim, secondClaim)
	}
	if _, err := VerificationTaskComplete(
		ctx,
		firstPairing.Token,
		firstClaim.TaskToken,
		[]VerificationCookieInput{{Name: "cf_clearance", Value: "old", Domain: ".example.com"}},
		"",
	); err == nil {
		t.Fatal("stale claim token should no longer complete the task")
	}
}

func TestVerificationTaskClaimReleasesExpiredPairing(t *testing.T) {
	ctx := setupBackupTestDB(t)
	_, account := createVerificationFixture(t, ctx)
	created, err := VerificationSessionCreate(ctx, VerificationSessionCreateRequest{
		SiteAccountID: account.ID,
	})
	if err != nil {
		t.Fatalf("create verification session: %v", err)
	}
	firstPairing, err := VerificationBridgePairingCreate(ctx, "expiring bridge", 1)
	if err != nil {
		t.Fatalf("create first pairing: %v", err)
	}
	firstClaim, err := VerificationTaskClaim(ctx, firstPairing.Token)
	if err != nil {
		t.Fatalf("claim task: %v", err)
	}
	if err := dbpkg.GetDB().WithContext(ctx).Model(&model.VerificationBridgePairing{}).
		Where("id = ?", firstPairing.Pairing.ID).
		Update("expires_at", time.Now().Add(-time.Second)).Error; err != nil {
		t.Fatalf("expire pairing: %v", err)
	}

	secondPairing, err := VerificationBridgePairingCreate(ctx, "replacement bridge", 1)
	if err != nil {
		t.Fatalf("create replacement pairing: %v", err)
	}
	secondClaim, err := VerificationTaskClaim(ctx, secondPairing.Token)
	if err != nil {
		t.Fatalf("reclaim task from expired pairing: %v", err)
	}
	if secondClaim.Task.ID != created.Task.ID ||
		secondClaim.TaskToken == firstClaim.TaskToken {
		t.Fatalf("expired pairing did not release task: first=%+v second=%+v", firstClaim, secondClaim)
	}
}

func TestVerificationTaskReleaseAllowsImmediateReclaim(t *testing.T) {
	ctx := setupBackupTestDB(t)
	_, account := createVerificationFixture(t, ctx)
	created, err := VerificationSessionCreate(ctx, VerificationSessionCreateRequest{
		SiteAccountID: account.ID,
	})
	if err != nil {
		t.Fatalf("create verification session: %v", err)
	}
	firstPairing, err := VerificationBridgePairingCreate(ctx, "first bridge", 1)
	if err != nil {
		t.Fatalf("create first pairing: %v", err)
	}
	firstClaim, err := VerificationTaskClaim(ctx, firstPairing.Token)
	if err != nil {
		t.Fatalf("claim task: %v", err)
	}
	if err := VerificationTaskRelease(ctx, firstPairing.Token, firstClaim.TaskToken); err != nil {
		t.Fatalf("release task: %v", err)
	}

	secondPairing, err := VerificationBridgePairingCreate(ctx, "second bridge", 1)
	if err != nil {
		t.Fatalf("create second pairing: %v", err)
	}
	secondClaim, err := VerificationTaskClaim(ctx, secondPairing.Token)
	if err != nil {
		t.Fatalf("reclaim released task: %v", err)
	}
	if secondClaim.Task.ID != created.Task.ID {
		t.Fatalf("reclaimed wrong task: got %d want %d", secondClaim.Task.ID, created.Task.ID)
	}
}

func TestVerificationSessionRevokeCancelsActiveTask(t *testing.T) {
	ctx := setupBackupTestDB(t)
	_, account := createVerificationFixture(t, ctx)
	created, err := VerificationSessionCreate(ctx, VerificationSessionCreateRequest{
		SiteAccountID: account.ID,
	})
	if err != nil {
		t.Fatalf("create verification session: %v", err)
	}
	pairing, err := VerificationBridgePairingCreate(ctx, "bridge", 1)
	if err != nil {
		t.Fatalf("create pairing: %v", err)
	}
	if _, err := VerificationTaskClaim(ctx, pairing.Token); err != nil {
		t.Fatalf("claim task: %v", err)
	}

	if err := VerificationSessionRevoke(ctx, created.Session.ID); err != nil {
		t.Fatalf("revoke session: %v", err)
	}
	var task model.VerificationTask
	if err := dbpkg.GetDB().WithContext(ctx).First(&task, created.Task.ID).Error; err != nil {
		t.Fatalf("reload task: %v", err)
	}
	if task.Status != model.VerificationTaskCanceled ||
		task.PairingID != nil ||
		task.ClaimTokenHash != "" ||
		task.ClaimedAt != nil {
		t.Fatalf("active task was not canceled with session: %+v", task)
	}
}

func TestVerificationSessionRevokeCancelsTaskAndClearsCredential(t *testing.T) {
	ctx := setupBackupTestDB(t)
	_, account := createVerificationFixture(t, ctx)
	created, err := VerificationSessionCreate(ctx, VerificationSessionCreateRequest{
		SiteAccountID: account.ID,
	})
	if err != nil {
		t.Fatalf("create verification session: %v", err)
	}
	pairing, err := VerificationBridgePairingCreate(ctx, "bridge", 1)
	if err != nil {
		t.Fatalf("create pairing: %v", err)
	}
	claimed, err := VerificationTaskClaim(ctx, pairing.Token)
	if err != nil {
		t.Fatalf("claim task: %v", err)
	}
	if _, err := VerificationTaskComplete(
		ctx,
		pairing.Token,
		claimed.TaskToken,
		[]VerificationCookieInput{
			{Name: "cf_clearance", Value: "verified", Domain: ".example.com"},
		},
		"",
	); err != nil {
		t.Fatalf("complete task: %v", err)
	}
	if _, err := VerificationTaskComplete(
		ctx,
		pairing.Token,
		claimed.TaskToken,
		[]VerificationCookieInput{
			{Name: "cf_clearance", Value: "verified", Domain: ".example.com"},
		},
		"",
	); err == nil {
		t.Fatal("completed task should reject duplicate submission")
	}

	if err := VerificationSessionRevoke(ctx, created.Session.ID); err != nil {
		t.Fatalf("revoke session: %v", err)
	}
	var reloadedSession model.VerificationSession
	if err := dbpkg.GetDB().WithContext(ctx).First(&reloadedSession, created.Session.ID).Error; err != nil {
		t.Fatalf("reload session: %v", err)
	}
	if reloadedSession.Status != model.VerificationSessionRevoked {
		t.Fatalf("session not revoked: %+v", reloadedSession)
	}
	var reloadedAccount model.SiteAccount
	if err := dbpkg.GetDB().WithContext(ctx).First(&reloadedAccount, account.ID).Error; err != nil {
		t.Fatalf("reload account: %v", err)
	}
	if reloadedAccount.VerificationCookieEncrypted != "" ||
		reloadedAccount.VerificationExpiresAt != nil {
		t.Fatalf("revoked session credential still active on account: %+v", reloadedAccount)
	}
}

func TestExpiredBridgeCompletionPersistsExpiredState(t *testing.T) {
	ctx := setupBackupTestDB(t)
	_, account := createVerificationFixture(t, ctx)
	created, err := VerificationSessionCreate(ctx, VerificationSessionCreateRequest{
		SiteAccountID: account.ID,
	})
	if err != nil {
		t.Fatalf("create verification session: %v", err)
	}
	pairing, err := VerificationBridgePairingCreate(ctx, "bridge", 1)
	if err != nil {
		t.Fatalf("create pairing: %v", err)
	}
	claimed, err := VerificationTaskClaim(ctx, pairing.Token)
	if err != nil {
		t.Fatalf("claim task: %v", err)
	}
	expiredAt := time.Now().Add(-time.Minute)
	if err := dbpkg.GetDB().WithContext(ctx).Model(&model.VerificationSession{}).
		Where("id = ?", created.Session.ID).
		Update("expires_at", expiredAt).Error; err != nil {
		t.Fatalf("expire session: %v", err)
	}
	if err := dbpkg.GetDB().WithContext(ctx).Model(&model.VerificationTask{}).
		Where("id = ?", created.Task.ID).
		Update("expires_at", expiredAt).Error; err != nil {
		t.Fatalf("expire task: %v", err)
	}

	if _, err := VerificationTaskComplete(
		ctx,
		pairing.Token,
		claimed.TaskToken,
		[]VerificationCookieInput{{Name: "cf_clearance", Value: "late", Domain: ".example.com"}},
		"",
	); err == nil {
		t.Fatal("expired bridge completion should fail")
	}
	var session model.VerificationSession
	if err := dbpkg.GetDB().WithContext(ctx).First(&session, created.Session.ID).Error; err != nil {
		t.Fatalf("reload expired session: %v", err)
	}
	if session.Status != model.VerificationSessionExpired {
		t.Fatalf("expired bridge session status was not persisted: %+v", session)
	}
	var task model.VerificationTask
	if err := dbpkg.GetDB().WithContext(ctx).First(&task, created.Task.ID).Error; err != nil {
		t.Fatalf("reload expired task: %v", err)
	}
	if task.Status != model.VerificationTaskExpired {
		t.Fatalf("expired bridge task status was not persisted: %+v", task)
	}
}

func TestExpiredManualCompletionPersistsExpiredState(t *testing.T) {
	ctx := setupBackupTestDB(t)
	_, account := createVerificationFixture(t, ctx)
	created, err := VerificationSessionCreate(ctx, VerificationSessionCreateRequest{
		SiteAccountID: account.ID,
	})
	if err != nil {
		t.Fatalf("create verification session: %v", err)
	}
	expiredAt := time.Now().Add(-time.Minute)
	if err := dbpkg.GetDB().WithContext(ctx).Model(&model.VerificationSession{}).
		Where("id = ?", created.Session.ID).
		Update("expires_at", expiredAt).Error; err != nil {
		t.Fatalf("expire session: %v", err)
	}
	if err := dbpkg.GetDB().WithContext(ctx).Model(&model.VerificationTask{}).
		Where("id = ?", created.Task.ID).
		Update("expires_at", expiredAt).Error; err != nil {
		t.Fatalf("expire task: %v", err)
	}

	if _, err := VerificationSessionManualComplete(ctx, created.Session.ID, "cf_clearance=value", ""); err == nil {
		t.Fatal("expired verification session completion should fail")
	}
	var session model.VerificationSession
	if err := dbpkg.GetDB().WithContext(ctx).First(&session, created.Session.ID).Error; err != nil {
		t.Fatalf("reload expired session: %v", err)
	}
	if session.Status != model.VerificationSessionExpired {
		t.Fatalf("expired session status was rolled back: %+v", session)
	}
	var task model.VerificationTask
	if err := dbpkg.GetDB().WithContext(ctx).First(&task, created.Task.ID).Error; err != nil {
		t.Fatalf("reload expired task: %v", err)
	}
	if task.Status != model.VerificationTaskExpired {
		t.Fatalf("expired task status was not persisted: %+v", task)
	}
}

func TestExpiredVerificationTaskCancelsPendingRetry(t *testing.T) {
	ctx := setupBackupTestDB(t)
	_, account := createVerificationFixture(t, ctx)
	created, err := VerificationSessionCreate(ctx, VerificationSessionCreateRequest{
		SiteAccountID: account.ID,
		Operation:     model.SiteOperationSync,
	})
	if err != nil {
		t.Fatalf("create verification session: %v", err)
	}
	expiredAt := time.Now().Add(-time.Minute)
	if err := dbpkg.GetDB().WithContext(ctx).Model(&model.VerificationSession{}).
		Where("id = ?", created.Session.ID).
		Update("expires_at", expiredAt).Error; err != nil {
		t.Fatalf("expire session: %v", err)
	}
	if err := dbpkg.GetDB().WithContext(ctx).Model(&model.VerificationTask{}).
		Where("id = ?", created.Task.ID).
		Update("expires_at", expiredAt).Error; err != nil {
		t.Fatalf("expire task: %v", err)
	}

	if _, err := VerificationSessionList(ctx, account.ID); err != nil {
		t.Fatalf("list verification sessions: %v", err)
	}
	var task model.VerificationTask
	if err := dbpkg.GetDB().WithContext(ctx).First(&task, created.Task.ID).Error; err != nil {
		t.Fatalf("reload expired verification task: %v", err)
	}
	if task.Status != model.VerificationTaskExpired ||
		task.RetryStatus != model.VerificationRetryCanceled ||
		task.RetryTokenHash != "" {
		t.Fatalf("expired verification task retained retry work: %+v", task)
	}
}

func TestManualCompletionRejectsUserAgentMismatch(t *testing.T) {
	ctx := setupBackupTestDB(t)
	_, account := createVerificationFixture(t, ctx)
	created, err := VerificationSessionCreate(ctx, VerificationSessionCreateRequest{
		SiteAccountID: account.ID,
		UserAgent:     "bound-agent",
	})
	if err != nil {
		t.Fatalf("create verification session: %v", err)
	}
	if _, err := VerificationSessionManualComplete(
		ctx,
		created.Session.ID,
		"cf_clearance=value",
		"other-agent",
	); err == nil {
		t.Fatal("manual completion should enforce the session user-agent binding")
	}
}

func TestVerificationRetryLifecycleRejectsStaleWorker(t *testing.T) {
	ctx := setupBackupTestDB(t)
	_, account := createVerificationFixture(t, ctx)
	created, err := VerificationSessionCreate(ctx, VerificationSessionCreateRequest{
		SiteAccountID: account.ID,
		Operation:     model.SiteOperationSync,
	})
	if err != nil {
		t.Fatalf("create verification session: %v", err)
	}
	if _, err := VerificationSessionManualComplete(
		ctx,
		created.Session.ID,
		"cf_clearance=value",
		"",
	); err != nil {
		t.Fatalf("complete verification session: %v", err)
	}

	first, err := VerificationRetryAcquire(ctx, created.Session.ID)
	if err != nil || first == nil {
		t.Fatalf("acquire first retry: work=%+v err=%v", first, err)
	}
	if duplicate, err := VerificationRetryAcquire(ctx, created.Session.ID); err != nil || duplicate != nil {
		t.Fatalf("active retry should not be acquired twice: work=%+v err=%v", duplicate, err)
	}

	staleAt := time.Now().Add(-verificationRetryStaleAfter - time.Second)
	if err := dbpkg.GetDB().WithContext(ctx).Model(&model.VerificationTask{}).
		Where("id = ?", first.Task.ID).
		Update("retry_started_at", staleAt).Error; err != nil {
		t.Fatalf("age retry lease: %v", err)
	}
	second, err := VerificationRetryAcquire(ctx, created.Session.ID)
	if err != nil || second == nil {
		t.Fatalf("reacquire stale retry: work=%+v err=%v", second, err)
	}
	if second.Token == first.Token {
		t.Fatal("reacquired retry reused the old worker token")
	}
	if err := VerificationRetryFinish(
		ctx,
		first.Task.ID,
		first.Token,
		model.VerificationRetrySucceeded,
		"stale worker",
	); err == nil {
		t.Fatal("stale retry worker should not overwrite the new run")
	}
	if err := VerificationRetryFinish(
		ctx,
		second.Task.ID,
		second.Token,
		model.VerificationRetrySucceeded,
		"retry completed",
	); err != nil {
		t.Fatalf("finish current retry: %v", err)
	}

	var task model.VerificationTask
	if err := dbpkg.GetDB().WithContext(ctx).First(&task, created.Task.ID).Error; err != nil {
		t.Fatalf("reload retry task: %v", err)
	}
	if task.RetryStatus != model.VerificationRetrySucceeded ||
		task.RetryTokenHash != "" ||
		task.RetryMessage != "retry completed" {
		t.Fatalf("unexpected retry completion state: %+v", task)
	}
	if err := VerificationRetryRequeue(ctx, created.Session.ID); err != nil {
		t.Fatalf("requeue retry: %v", err)
	}
}

func TestVerificationRetryPendingSessionIDsIncludesPendingAndStaleRunning(t *testing.T) {
	ctx := setupBackupTestDB(t)
	_, account := createVerificationFixture(t, ctx)

	createRetry := func() *VerificationSessionCreated {
		created, err := VerificationSessionCreate(ctx, VerificationSessionCreateRequest{
			SiteAccountID: account.ID,
			Operation:     model.SiteOperationSync,
		})
		if err != nil {
			t.Fatalf("create verification retry: %v", err)
		}
		if _, err := VerificationSessionManualComplete(
			ctx,
			created.Session.ID,
			"cf_clearance=value",
			"",
		); err != nil {
			t.Fatalf("complete verification retry: %v", err)
		}
		return created
	}

	pending := createRetry()
	active := createRetry()
	stale := createRetry()
	if work, err := VerificationRetryAcquire(ctx, active.Session.ID); err != nil || work == nil {
		t.Fatalf("acquire active retry: work=%+v err=%v", work, err)
	}
	if work, err := VerificationRetryAcquire(ctx, stale.Session.ID); err != nil || work == nil {
		t.Fatalf("acquire stale retry: work=%+v err=%v", work, err)
	}
	if err := dbpkg.GetDB().WithContext(ctx).Model(&model.VerificationTask{}).
		Where("session_id = ?", stale.Session.ID).
		Update("retry_started_at", time.Now().Add(-verificationRetryStaleAfter-time.Second)).Error; err != nil {
		t.Fatalf("age stale retry: %v", err)
	}

	sessionIDs, err := VerificationRetryPendingSessionIDs(ctx, 10)
	if err != nil {
		t.Fatalf("list pending verification retries: %v", err)
	}
	found := make(map[int64]bool, len(sessionIDs))
	for _, sessionID := range sessionIDs {
		found[sessionID] = true
	}
	if !found[pending.Session.ID] || !found[stale.Session.ID] {
		t.Fatalf("pending sweep missed recoverable work: ids=%v", sessionIDs)
	}
	if found[active.Session.ID] {
		t.Fatalf("pending sweep selected an active retry: ids=%v", sessionIDs)
	}
}

func TestClashSwitchLeaseRejectsStaleOwnerRelease(t *testing.T) {
	ctx := setupBackupTestDB(t)
	controller := model.ClashController{
		Name:      "lease-controller",
		APIURL:    "http://127.0.0.1:19090",
		ProxyURL:  "http://127.0.0.1:19091",
		GroupName: "Octopus",
		Enabled:   true,
	}
	if err := dbpkg.GetDB().WithContext(ctx).Create(&controller).Error; err != nil {
		t.Fatalf("create controller: %v", err)
	}
	first, err := acquireClashSwitchLease(ctx, &controller)
	if err != nil {
		t.Fatalf("acquire first lease: %v", err)
	}
	past := time.Now().Add(-time.Second)
	if err := dbpkg.GetDB().WithContext(ctx).Model(&model.ClashSwitchLease{}).
		Where("lease_key = ?", first.key).
		Update("expires_at", past).Error; err != nil {
		t.Fatalf("expire first lease: %v", err)
	}
	second, err := acquireClashSwitchLease(ctx, &controller)
	if err != nil {
		t.Fatalf("replace expired lease: %v", err)
	}

	if err := releaseClashSwitchLease(ctx, first); err != nil {
		t.Fatalf("release stale lease owner: %v", err)
	}
	var count int64
	if err := dbpkg.GetDB().WithContext(ctx).Model(&model.ClashSwitchLease{}).
		Where("lease_key = ? AND owner_token = ?", second.key, second.owner).
		Count(&count).Error; err != nil {
		t.Fatalf("count current lease: %v", err)
	}
	if count != 1 {
		t.Fatal("stale lease owner released the current lease")
	}
	if err := releaseClashSwitchLease(ctx, second); err != nil {
		t.Fatalf("release current lease owner: %v", err)
	}
}

func TestClashSwitchNodeWaitsForControllerConfirmation(t *testing.T) {
	ctx := setupBackupTestDB(t)
	var mu sync.Mutex
	current := "A"
	putSeen := false
	confirmReads := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		switch r.Method {
		case http.MethodGet:
			if putSeen {
				confirmReads++
				if confirmReads >= 2 {
					current = "B"
				}
			}
			_, _ = w.Write([]byte(`{"now":"` + current + `","all":["A","B"]}`))
		case http.MethodPut:
			putSeen = true
			w.WriteHeader(http.StatusNoContent)
		default:
			t.Fatalf("unexpected method: %s", r.Method)
		}
	}))
	defer server.Close()

	controller := model.ClashController{
		Name:      "confirm-controller",
		APIURL:    server.URL,
		ProxyURL:  "http://127.0.0.1:19092",
		GroupName: "Octopus",
		Enabled:   true,
	}
	if err := dbpkg.GetDB().WithContext(ctx).Create(&controller).Error; err != nil {
		t.Fatalf("create controller: %v", err)
	}
	if err := ClashSwitchNode(ctx, controller.ID, "B"); err != nil {
		t.Fatalf("switch node: %v", err)
	}
	if confirmReads != 2 {
		t.Fatalf("expected switch confirmation retry, got %d reads", confirmReads)
	}
}

func TestSiteProxyPreferenceCooldownExpiryAndSuccessRecovery(t *testing.T) {
	ctx := setupBackupTestDB(t)
	path := SiteProxyPathDescriptor{
		SiteID:        11,
		SiteAccountID: 22,
		ProxyMode:     model.ProxyUsageModePool,
		ProxyConfigID: 33,
		ClashNode:     "node-a",
	}
	if err := SiteProxyPreferenceRecordFailure(ctx, path, "network", 250*time.Millisecond); err != nil {
		t.Fatalf("record failure: %v", err)
	}
	var failed model.SiteProxyPreference
	if err := dbpkg.GetDB().WithContext(ctx).
		Where("identity_key = ?", path.IdentityKey()).
		First(&failed).Error; err != nil {
		t.Fatalf("load failed preference: %v", err)
	}
	if failed.CooldownUntil == nil || SiteProxyPreferenceUsable(failed, time.Now()) {
		t.Fatalf("failed path should be cooling: %+v", failed)
	}
	if !SiteProxyPreferenceUsable(failed, failed.CooldownUntil.Add(time.Nanosecond)) {
		t.Fatal("path should become eligible after cooldown")
	}

	if err := SiteProxyPreferenceRecordSuccess(ctx, path, 100*time.Millisecond); err != nil {
		t.Fatalf("record success: %v", err)
	}
	var recovered model.SiteProxyPreference
	if err := dbpkg.GetDB().WithContext(ctx).
		Where("identity_key = ?", path.IdentityKey()).
		First(&recovered).Error; err != nil {
		t.Fatalf("load recovered preference: %v", err)
	}
	if recovered.Status != model.SiteProxyPreferenceHealthy ||
		recovered.CooldownUntil != nil ||
		recovered.ConsecutiveFailures != 0 ||
		recovered.SuccessCount != 1 ||
		recovered.FailureCount != 1 {
		t.Fatalf("success did not restore preference health: %+v", recovered)
	}

	expired := time.Now().Add(-time.Second)
	recovered.ExpiresAt = &expired
	if SiteProxyPreferenceUsable(recovered, time.Now()) {
		t.Fatal("expired preference should not be usable")
	}
}

func createVerificationFixture(t *testing.T, ctx context.Context) (model.Site, model.SiteAccount) {
	t.Helper()
	settingCache.Set(model.SettingKeyJWTSecret, "verification-test-secret")
	site := model.Site{
		Name:              "verification-site",
		Platform:          model.SitePlatformNewAPI,
		BaseURL:           "https://api.example.com",
		Enabled:           true,
		ProxyMode:         model.ProxyUsageModeDirect,
		AutoProxyRecovery: true,
	}
	if err := dbpkg.GetDB().WithContext(ctx).Create(&site).Error; err != nil {
		t.Fatalf("create site: %v", err)
	}
	account := model.SiteAccount{
		SiteID:         site.ID,
		Name:           "verification-account",
		CredentialType: model.SiteCredentialTypeAccessToken,
		AccessToken:    "test-token",
		ProxyMode:      model.ProxyUsageModeInherit,
		Enabled:        true,
	}
	if err := dbpkg.GetDB().WithContext(ctx).Create(&account).Error; err != nil {
		t.Fatalf("create account: %v", err)
	}
	return site, account
}
