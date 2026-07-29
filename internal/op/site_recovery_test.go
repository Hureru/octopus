package op

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
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

func TestVerificationSessionEnsureIsSingleCreator(t *testing.T) {
	ctx := setupBackupTestDB(t)
	_, account := createVerificationFixture(t, ctx)

	start := make(chan struct{})
	results := make(chan *VerificationSessionCreated, 2)
	errors := make(chan error, 2)
	var wait sync.WaitGroup
	for range 2 {
		wait.Add(1)
		go func() {
			defer wait.Done()
			<-start
			created, err := VerificationSessionEnsure(ctx, VerificationSessionCreateRequest{
				SiteAccountID: account.ID,
			})
			if err != nil {
				errors <- err
				return
			}
			results <- created
		}()
	}
	close(start)
	wait.Wait()
	close(results)
	close(errors)

	for err := range errors {
		t.Fatalf("concurrent ensure failed: %v", err)
	}
	var first *VerificationSessionCreated
	for result := range results {
		if first == nil {
			first = result
			continue
		}
		if result.Session.ID != first.Session.ID || result.Task.ID != first.Task.ID {
			t.Fatalf("concurrent ensure created duplicate work: first=%+v second=%+v", first, result)
		}
	}
	if first == nil {
		t.Fatal("concurrent ensure returned no result")
	}

	var sessionCount, taskCount int64
	if err := dbpkg.GetDB().WithContext(ctx).Model(&model.VerificationSession{}).
		Where("site_account_id = ?", account.ID).
		Count(&sessionCount).Error; err != nil {
		t.Fatalf("count sessions: %v", err)
	}
	if err := dbpkg.GetDB().WithContext(ctx).Model(&model.VerificationTask{}).
		Where("session_id = ?", first.Session.ID).
		Count(&taskCount).Error; err != nil {
		t.Fatalf("count tasks: %v", err)
	}
	if sessionCount != 1 || taskCount != 1 {
		t.Fatalf("concurrent ensure persisted sessions=%d tasks=%d", sessionCount, taskCount)
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
	pairing, err := VerificationBridgePairingCreate(ctx, "test bridge", 1, account.ID)
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

func TestVerificationTaskClaimIsScopedToPairingAccount(t *testing.T) {
	ctx := setupBackupTestDB(t)
	site, accountA := createVerificationFixture(t, ctx)
	accountB := model.SiteAccount{
		SiteID:         site.ID,
		Name:           "verification-account-b",
		CredentialType: model.SiteCredentialTypeAccessToken,
		AccessToken:    "test-token-b",
		ProxyMode:      model.ProxyUsageModeInherit,
		Enabled:        true,
	}
	if err := dbpkg.GetDB().WithContext(ctx).Create(&accountB).Error; err != nil {
		t.Fatalf("create second account: %v", err)
	}
	createdB, err := VerificationSessionCreate(ctx, VerificationSessionCreateRequest{
		SiteAccountID: accountB.ID,
	})
	if err != nil {
		t.Fatalf("create account B verification session: %v", err)
	}
	createdA, err := VerificationSessionCreate(ctx, VerificationSessionCreateRequest{
		SiteAccountID: accountA.ID,
	})
	if err != nil {
		t.Fatalf("create account A verification session: %v", err)
	}
	pairingA, err := VerificationBridgePairingCreate(ctx, "account A bridge", 1, accountA.ID)
	if err != nil {
		t.Fatalf("create account A pairing: %v", err)
	}

	claimedA, err := VerificationTaskClaim(ctx, pairingA.Token)
	if err != nil {
		t.Fatalf("claim account A task: %v", err)
	}
	if claimedA.Task.ID != createdA.Task.ID || claimedA.Task.SessionID != createdA.Session.ID {
		t.Fatalf("account A pairing claimed cross-account task: got=%+v want=%+v", claimedA.Task, createdA.Task)
	}
	var untouchedB model.VerificationTask
	if err := dbpkg.GetDB().WithContext(ctx).First(&untouchedB, createdB.Task.ID).Error; err != nil {
		t.Fatalf("reload account B task: %v", err)
	}
	if untouchedB.Status != model.VerificationTaskPending || untouchedB.PairingID != nil {
		t.Fatalf("account B task was changed by account A pairing: %+v", untouchedB)
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

	header, err = verificationCookieHeader("api.example.com", []VerificationCookieInput{
		{Name: "session", Value: "must-not-leave-browser", Domain: ".example.com"},
		{Name: "__cf_bm", Value: "challenge", Domain: ".example.com"},
	})
	if err != nil {
		t.Fatalf("supported Cloudflare cookie was rejected: %v", err)
	}
	if header != "__cf_bm=challenge" || strings.Contains(header, "session") {
		t.Fatalf("non-Cloudflare cookies were not minimized: %q", header)
	}
	if _, err := verificationCookieHeader("api.example.com", []VerificationCookieInput{
		{Name: "session", Value: "browser-login", Domain: ".example.com"},
	}); err == nil {
		t.Fatal("session-only cookie jar should not be accepted")
	}
	if _, err := verificationCookieHeader("api.example.com", []VerificationCookieInput{
		{Name: "CF_CLEARANCE", Value: "name-collision", Domain: ".example.com"},
	}); err == nil {
		t.Fatal("case-variant cookie name should not be accepted as a Cloudflare credential")
	}

	header, err = verificationCookieHeader("api.example.com", []VerificationCookieInput{
		{Name: "cf_clearance", Value: "parent", Domain: ".example.com", Path: "/"},
		{Name: "cf_clearance", Value: "host", Domain: "api.example.com", Path: "/api"},
	})
	if err != nil {
		t.Fatalf("scoped duplicate Cloudflare cookies were rejected: %v", err)
	}
	if header != "cf_clearance=host; cf_clearance=parent" {
		t.Fatalf("scoped cookies were not ordered most-specific first: %q", header)
	}
	if _, err := verificationCookieHeader("api.example.com", []VerificationCookieInput{
		{Name: "cf_clearance", Value: "first", Domain: ".example.com", Path: "/"},
		{Name: "cf_clearance", Value: "second", Domain: ".example.com", Path: "/"},
	}); err == nil {
		t.Fatal("conflicting values for the same cookie scope should be rejected")
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
	firstPairing, err := VerificationBridgePairingCreate(ctx, "first bridge", 1, account.ID)
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

	secondPairing, err := VerificationBridgePairingCreate(ctx, "second bridge", 1, account.ID)
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
	firstPairing, err := VerificationBridgePairingCreate(ctx, "stale bridge", 1, account.ID)
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

	secondPairing, err := VerificationBridgePairingCreate(ctx, "replacement bridge", 1, account.ID)
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

func TestVerificationTaskClaimReturnsLeaseDeadline(t *testing.T) {
	ctx := setupBackupTestDB(t)
	_, account := createVerificationFixture(t, ctx)
	if _, err := VerificationSessionCreate(ctx, VerificationSessionCreateRequest{
		SiteAccountID: account.ID,
	}); err != nil {
		t.Fatalf("create verification session: %v", err)
	}
	pairing, err := VerificationBridgePairingCreate(ctx, "lease bridge", 1, account.ID)
	if err != nil {
		t.Fatalf("create pairing: %v", err)
	}
	before := time.Now()
	claimed, err := VerificationTaskClaim(ctx, pairing.Token)
	if err != nil {
		t.Fatalf("claim task: %v", err)
	}
	if claimed.ClaimExpiresAt.Before(before.Add(verificationTaskClaimTTL-time.Second)) ||
		claimed.ClaimExpiresAt.After(claimed.Task.ExpiresAt) {
		t.Fatalf("unexpected claim deadline: %+v", claimed)
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
	firstPairing, err := VerificationBridgePairingCreate(ctx, "expiring bridge", 1, account.ID)
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

	secondPairing, err := VerificationBridgePairingCreate(ctx, "replacement bridge", 1, account.ID)
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
	firstPairing, err := VerificationBridgePairingCreate(ctx, "first bridge", 1, account.ID)
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

	secondPairing, err := VerificationBridgePairingCreate(ctx, "second bridge", 1, account.ID)
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
	pairing, err := VerificationBridgePairingCreate(ctx, "bridge", 1, account.ID)
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
	pairing, err := VerificationBridgePairingCreate(ctx, "bridge", 1, account.ID)
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
	pairing, err := VerificationBridgePairingCreate(ctx, "bridge", 1, account.ID)
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

func TestExpiredBridgeCompletionDoesNotRequeueStaleClaim(t *testing.T) {
	ctx := setupBackupTestDB(t)
	_, account := createVerificationFixture(t, ctx)
	created, err := VerificationSessionCreate(ctx, VerificationSessionCreateRequest{
		SiteAccountID: account.ID,
	})
	if err != nil {
		t.Fatalf("create verification session: %v", err)
	}
	pairing, err := VerificationBridgePairingCreate(ctx, "bridge", 1, account.ID)
	if err != nil {
		t.Fatalf("create pairing: %v", err)
	}
	claimed, err := VerificationTaskClaim(ctx, pairing.Token)
	if err != nil {
		t.Fatalf("claim task: %v", err)
	}
	expiredAt := time.Now().Add(-time.Minute)
	staleClaimedAt := time.Now().Add(-verificationTaskClaimTTL - time.Minute)
	if err := dbpkg.GetDB().WithContext(ctx).Model(&model.VerificationSession{}).
		Where("id = ?", created.Session.ID).
		Update("expires_at", expiredAt).Error; err != nil {
		t.Fatalf("expire session: %v", err)
	}
	if err := dbpkg.GetDB().WithContext(ctx).Model(&model.VerificationTask{}).
		Where("id = ?", created.Task.ID).
		Updates(map[string]any{
			"expires_at": expiredAt,
			"claimed_at": staleClaimedAt,
		}).Error; err != nil {
		t.Fatalf("expire claimed task: %v", err)
	}

	if _, err := VerificationTaskComplete(
		ctx,
		pairing.Token,
		claimed.TaskToken,
		[]VerificationCookieInput{{Name: "cf_clearance", Value: "late", Domain: ".example.com"}},
		"",
	); !errors.Is(err, errVerificationSessionExpired) {
		t.Fatalf("completion error = %v, want expired session", err)
	}
	var task model.VerificationTask
	if err := dbpkg.GetDB().WithContext(ctx).First(&task, created.Task.ID).Error; err != nil {
		t.Fatalf("reload task: %v", err)
	}
	if task.Status != model.VerificationTaskExpired || task.ClaimedAt != nil {
		t.Fatalf("expired stale claim was requeued: %+v", task)
	}
}

func TestOlderVerificationSessionCannotReplaceNewerCredential(t *testing.T) {
	ctx := setupBackupTestDB(t)
	_, account := createVerificationFixture(t, ctx)
	older, err := VerificationSessionCreate(ctx, VerificationSessionCreateRequest{
		SiteAccountID: account.ID,
	})
	if err != nil {
		t.Fatalf("create older session: %v", err)
	}
	newer, err := VerificationSessionCreate(ctx, VerificationSessionCreateRequest{
		SiteAccountID: account.ID,
	})
	if err != nil {
		t.Fatalf("create newer session: %v", err)
	}
	if _, err := VerificationSessionManualComplete(
		ctx,
		newer.Session.ID,
		"cf_clearance=newer",
		"",
	); err != nil {
		t.Fatalf("complete newer session: %v", err)
	}
	if _, err := VerificationSessionManualComplete(
		ctx,
		older.Session.ID,
		"cf_clearance=older",
		"",
	); !errors.Is(err, errVerificationSessionSuperseded) {
		t.Fatalf("older completion error = %v, want superseded", err)
	}

	var reloadedAccount model.SiteAccount
	if err := dbpkg.GetDB().WithContext(ctx).First(&reloadedAccount, account.ID).Error; err != nil {
		t.Fatalf("reload account: %v", err)
	}
	if reloadedAccount.VerificationSessionFenceID != newer.Session.ID {
		t.Fatalf("credential fence = %d, want %d", reloadedAccount.VerificationSessionFenceID, newer.Session.ID)
	}
	cookie, err := DecryptSecret(reloadedAccount.VerificationCookieEncrypted)
	if err != nil {
		t.Fatalf("decrypt active credential: %v", err)
	}
	if cookie != "cf_clearance=newer" {
		t.Fatalf("older session replaced active credential: %q", cookie)
	}
	var reloadedOlder model.VerificationSession
	if err := dbpkg.GetDB().WithContext(ctx).First(&reloadedOlder, older.Session.ID).Error; err != nil {
		t.Fatalf("reload older session: %v", err)
	}
	if reloadedOlder.Status != model.VerificationSessionSuperseded ||
		reloadedOlder.CookieEncrypted != "" {
		t.Fatalf("superseded session retained credential: %+v", reloadedOlder)
	}
}

func TestRevokingCredentialRetainsVerificationFence(t *testing.T) {
	ctx := setupBackupTestDB(t)
	_, account := createVerificationFixture(t, ctx)
	older, err := VerificationSessionCreate(ctx, VerificationSessionCreateRequest{
		SiteAccountID: account.ID,
	})
	if err != nil {
		t.Fatalf("create older session: %v", err)
	}
	newer, err := VerificationSessionCreate(ctx, VerificationSessionCreateRequest{
		SiteAccountID: account.ID,
	})
	if err != nil {
		t.Fatalf("create newer session: %v", err)
	}
	if _, err := VerificationSessionManualComplete(
		ctx,
		newer.Session.ID,
		"cf_clearance=newer",
		"",
	); err != nil {
		t.Fatalf("complete newer session: %v", err)
	}
	if err := VerificationSessionRevoke(ctx, newer.Session.ID); err != nil {
		t.Fatalf("revoke newer session: %v", err)
	}
	if _, err := VerificationSessionManualComplete(
		ctx,
		older.Session.ID,
		"cf_clearance=older",
		"",
	); !errors.Is(err, errVerificationSessionSuperseded) {
		t.Fatalf("older completion error = %v, want superseded", err)
	}
	var reloaded model.SiteAccount
	if err := dbpkg.GetDB().WithContext(ctx).First(&reloaded, account.ID).Error; err != nil {
		t.Fatalf("reload account: %v", err)
	}
	if reloaded.VerificationSessionFenceID != newer.Session.ID ||
		reloaded.VerificationCookieEncrypted != "" {
		t.Fatalf("revoke lowered fence or retained credential: %+v", reloaded)
	}
}

func TestVerificationCompletionExtendsCredentialFromCompletionTime(t *testing.T) {
	ctx := setupBackupTestDB(t)
	_, account := createVerificationFixture(t, ctx)
	created, err := VerificationSessionCreate(ctx, VerificationSessionCreateRequest{
		SiteAccountID: account.ID,
		TTLMinutes:    15,
	})
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	createdAt := time.Now().Add(-10 * time.Minute)
	originalExpiry := createdAt.Add(15 * time.Minute)
	if err := dbpkg.GetDB().WithContext(ctx).Model(&model.VerificationSession{}).
		Where("id = ?", created.Session.ID).
		Updates(map[string]any{
			"created_at": createdAt,
			"expires_at": originalExpiry,
		}).Error; err != nil {
		t.Fatalf("age pending session: %v", err)
	}
	before := time.Now()
	completed, err := VerificationSessionManualComplete(
		ctx,
		created.Session.ID,
		"cf_clearance=value",
		"",
	)
	if err != nil {
		t.Fatalf("complete session: %v", err)
	}
	if completed.ExpiresAt.Before(before.Add(15*time.Minute - time.Second)) {
		t.Fatalf(
			"credential expiry was not extended from completion: got %s, original %s",
			completed.ExpiresAt,
			originalExpiry,
		)
	}
}

func TestVerificationSessionCleanupErasesExpiredCredential(t *testing.T) {
	ctx := setupBackupTestDB(t)
	_, account := createVerificationFixture(t, ctx)
	created, err := VerificationSessionCreate(ctx, VerificationSessionCreateRequest{
		SiteAccountID: account.ID,
		Operation:     model.SiteOperationSync,
	})
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	if _, err := VerificationSessionManualComplete(
		ctx,
		created.Session.ID,
		"cf_clearance=value",
		"",
	); err != nil {
		t.Fatalf("complete session: %v", err)
	}
	expiredAt := time.Now().Add(-time.Minute)
	if err := dbpkg.GetDB().WithContext(ctx).Model(&model.VerificationSession{}).
		Where("id = ?", created.Session.ID).
		Update("expires_at", expiredAt).Error; err != nil {
		t.Fatalf("expire completed session: %v", err)
	}
	if err := dbpkg.GetDB().WithContext(ctx).Model(&model.SiteAccount{}).
		Where("id = ?", account.ID).
		Update("verification_expires_at", expiredAt).Error; err != nil {
		t.Fatalf("expire account credential: %v", err)
	}

	cleaned, err := VerificationSessionCleanup(ctx, time.Now())
	if err != nil {
		t.Fatalf("cleanup verification sessions: %v", err)
	}
	if cleaned != 1 {
		t.Fatalf("cleaned %d sessions, want 1", cleaned)
	}
	var session model.VerificationSession
	if err := dbpkg.GetDB().WithContext(ctx).First(&session, created.Session.ID).Error; err != nil {
		t.Fatalf("reload session: %v", err)
	}
	if session.Status != model.VerificationSessionExpired || session.CookieEncrypted != "" {
		t.Fatalf("expired session retained credential: %+v", session)
	}
	var reloadedAccount model.SiteAccount
	if err := dbpkg.GetDB().WithContext(ctx).First(&reloadedAccount, account.ID).Error; err != nil {
		t.Fatalf("reload account: %v", err)
	}
	if reloadedAccount.VerificationCookieEncrypted != "" ||
		reloadedAccount.VerificationSessionFenceID != created.Session.ID {
		t.Fatalf("cleanup lost fence or retained credential: %+v", reloadedAccount)
	}
	var task model.VerificationTask
	if err := dbpkg.GetDB().WithContext(ctx).First(&task, created.Task.ID).Error; err != nil {
		t.Fatalf("reload task: %v", err)
	}
	if task.RetryStatus != model.VerificationRetryCanceled {
		t.Fatalf("expired session retained retry work: %+v", task)
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

func TestClashOperationGuardCoversCallerRequestLifetime(t *testing.T) {
	ctx := setupBackupTestDB(t)
	var mu sync.Mutex
	current := "A"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		switch r.Method {
		case http.MethodGet:
			_, _ = w.Write([]byte(`{"now":"` + current + `","all":["A","B"]}`))
		case http.MethodPut:
			current = "B"
			w.WriteHeader(http.StatusNoContent)
		default:
			t.Fatalf("unexpected method: %s", r.Method)
		}
	}))
	defer server.Close()

	controller := model.ClashController{
		Name:      "operation-guard-controller",
		APIURL:    server.URL,
		ProxyURL:  "http://127.0.0.1:19093",
		GroupName: "Octopus",
		Enabled:   true,
	}
	if err := dbpkg.GetDB().WithContext(ctx).Create(&controller).Error; err != nil {
		t.Fatalf("create controller: %v", err)
	}
	releaseA, err := ClashSwitchNodeForOperation(ctx, controller.ID, "A")
	if err != nil {
		t.Fatalf("hold node A: %v", err)
	}
	acquiredB := make(chan func(), 1)
	errorsB := make(chan error, 1)
	go func() {
		releaseB, switchErr := ClashSwitchNodeForOperation(ctx, controller.ID, "B")
		if switchErr != nil {
			errorsB <- switchErr
			return
		}
		acquiredB <- releaseB
	}()

	select {
	case releaseB := <-acquiredB:
		releaseB()
		releaseA()
		t.Fatal("second operation switched nodes before the first request released its guard")
	case err := <-errorsB:
		releaseA()
		t.Fatalf("second operation failed while waiting: %v", err)
	case <-time.After(50 * time.Millisecond):
	}

	releaseA()
	select {
	case releaseB := <-acquiredB:
		releaseB()
	case err := <-errorsB:
		t.Fatalf("second operation failed after release: %v", err)
	case <-time.After(time.Second):
		t.Fatal("second operation did not acquire guard after release")
	}
}

func TestClashOperationGuardWaitHonorsContextCancellation(t *testing.T) {
	ctx := setupBackupTestDB(t)
	controller := model.ClashController{
		Name:      "cancelable-operation-guard",
		APIURL:    "http://127.0.0.1:1",
		ProxyURL:  "http://127.0.0.1:19094",
		GroupName: "Octopus",
		Enabled:   true,
	}
	if err := dbpkg.GetDB().WithContext(ctx).Create(&controller).Error; err != nil {
		t.Fatalf("create controller: %v", err)
	}
	release, err := acquireClashOperationGuard(ctx, &controller)
	if err != nil {
		t.Fatalf("acquire first guard: %v", err)
	}
	defer release()

	waitCtx, cancel := context.WithTimeout(ctx, 30*time.Millisecond)
	defer cancel()
	startedAt := time.Now()
	_, err = acquireClashOperationGuard(waitCtx, &controller)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("guard wait error = %v", err)
	}
	if time.Since(startedAt) > 250*time.Millisecond {
		t.Fatal("guard wait ignored context deadline")
	}
}

func TestClashControllerHTTPClientIsBounded(t *testing.T) {
	client, err := newClashControllerHTTPClient()
	if err != nil {
		t.Fatalf("create controller client: %v", err)
	}
	if client.Timeout != clashControllerHTTPTimeout || client.Timeout <= 0 {
		t.Fatalf("controller client timeout = %s", client.Timeout)
	}
}

func TestClashControllerUpsertRejectsGlobalGroup(t *testing.T) {
	ctx := setupBackupTestDB(t)
	for _, groupName := range []string{"GLOBAL", "global", " Global "} {
		t.Run(groupName, func(t *testing.T) {
			_, err := ClashControllerUpsert(ctx, ClashControllerInput{
				Name:      "global-group-" + strings.TrimSpace(groupName),
				APIURL:    "http://127.0.0.1:9090",
				ProxyURL:  "http://127.0.0.1:7890",
				GroupName: groupName,
				Enabled:   true,
			})
			if err == nil || !strings.Contains(err.Error(), "not GLOBAL") {
				t.Fatalf("GLOBAL group should be rejected, got %v", err)
			}
		})
	}
}

func TestClashControllerOperationRejectsLegacyGlobalGroup(t *testing.T) {
	ctx := setupBackupTestDB(t)
	controller := model.ClashController{
		Name:      "legacy-global",
		APIURL:    "http://127.0.0.1:9090",
		ProxyURL:  "http://127.0.0.1:7890",
		GroupName: "GLOBAL",
		Enabled:   true,
	}
	if err := dbpkg.GetDB().WithContext(ctx).Create(&controller).Error; err != nil {
		t.Fatalf("seed legacy GLOBAL controller: %v", err)
	}
	release, err := ClashSwitchNodeForOperation(ctx, controller.ID, "node-a")
	if release != nil {
		release()
	}
	if err == nil || !strings.Contains(err.Error(), "not GLOBAL") {
		t.Fatalf("legacy GLOBAL controller should be blocked at runtime, got %v", err)
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

func TestSiteProxyPreferenceClearSiteKeepsAccountPreferences(t *testing.T) {
	ctx := setupBackupTestDB(t)
	site, account := createVerificationFixture(t, ctx)
	proxyID := 41
	if err := dbpkg.GetDB().WithContext(ctx).Model(&model.Site{}).
		Where("id = ?", site.ID).
		Updates(map[string]any{
			"preferred_proxy_config_id": proxyID,
			"preferred_clash_node":      "site-node",
		}).Error; err != nil {
		t.Fatalf("seed site preference fields: %v", err)
	}
	if err := dbpkg.GetDB().WithContext(ctx).Model(&model.SiteAccount{}).
		Where("id = ?", account.ID).
		Updates(map[string]any{
			"preferred_proxy_config_id": proxyID + 1,
			"preferred_clash_node":      "account-node",
		}).Error; err != nil {
		t.Fatalf("seed account preference fields: %v", err)
	}
	for _, descriptor := range []SiteProxyPathDescriptor{
		{
			SiteID:        site.ID,
			SiteAccountID: 0,
			ProxyMode:     model.ProxyUsageModePool,
			ProxyConfigID: proxyID,
			ClashNode:     "site-node",
		},
		{
			SiteID:        site.ID,
			SiteAccountID: account.ID,
			ProxyMode:     model.ProxyUsageModePool,
			ProxyConfigID: proxyID + 1,
			ClashNode:     "account-node",
		},
	} {
		if err := SiteProxyPreferenceRecordSuccess(ctx, descriptor, time.Millisecond); err != nil {
			t.Fatalf("seed path preference: %v", err)
		}
	}

	if err := SiteProxyPreferenceClearSite(ctx, site.ID); err != nil {
		t.Fatalf("clear site preference: %v", err)
	}
	var siteCount, accountCount int64
	if err := dbpkg.GetDB().WithContext(ctx).Model(&model.SiteProxyPreference{}).
		Where("site_id = ? AND site_account_id = 0", site.ID).
		Count(&siteCount).Error; err != nil {
		t.Fatalf("count site preferences: %v", err)
	}
	if err := dbpkg.GetDB().WithContext(ctx).Model(&model.SiteProxyPreference{}).
		Where("site_id = ? AND site_account_id = ?", site.ID, account.ID).
		Count(&accountCount).Error; err != nil {
		t.Fatalf("count account preferences: %v", err)
	}
	if siteCount != 0 || accountCount != 1 {
		t.Fatalf("site clear crossed account boundary: site=%d account=%d", siteCount, accountCount)
	}

	var reloadedSite model.Site
	var reloadedAccount model.SiteAccount
	if err := dbpkg.GetDB().WithContext(ctx).First(&reloadedSite, site.ID).Error; err != nil {
		t.Fatalf("reload site: %v", err)
	}
	if err := dbpkg.GetDB().WithContext(ctx).First(&reloadedAccount, account.ID).Error; err != nil {
		t.Fatalf("reload account: %v", err)
	}
	if reloadedSite.PreferredProxyConfigID != nil || reloadedSite.PreferredClashNode != "" {
		t.Fatalf("site preference fields were not cleared: %+v", reloadedSite)
	}
	if reloadedAccount.PreferredProxyConfigID == nil ||
		*reloadedAccount.PreferredProxyConfigID != proxyID+1 ||
		reloadedAccount.PreferredClashNode != "account-node" {
		t.Fatalf("account preference fields were changed: %+v", reloadedAccount)
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
