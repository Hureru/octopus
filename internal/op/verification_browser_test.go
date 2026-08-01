package op

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	dbpkg "github.com/bestruirui/octopus/internal/db"
	"github.com/bestruirui/octopus/internal/model"
)

func TestVerificationBridgeIdentifyAndRotate(t *testing.T) {
	ctx := setupBackupTestDB(t)
	site, account := createVerificationFixture(t, ctx)
	created, err := VerificationBridgePairingCreate(ctx, "Chrome", 30, account.ID)
	if err != nil {
		t.Fatalf("create pairing: %v", err)
	}

	identity, err := VerificationBridgeIdentify(ctx, created.Token)
	if err != nil {
		t.Fatalf("identify pairing: %v", err)
	}
	if identity.Pairing.ID != created.Pairing.ID ||
		identity.SiteID != site.ID ||
		identity.SiteName != site.Name ||
		identity.SiteAccountID != account.ID ||
		identity.SiteAccountName != account.Name ||
		identity.Pairing.LastSeenAt == nil {
		t.Fatalf("unexpected pairing identity: %+v", identity)
	}

	rotated, err := VerificationBridgePairingRotate(ctx, created.Pairing.ID)
	if err != nil {
		t.Fatalf("rotate pairing token: %v", err)
	}
	if rotated.Pairing.ID != created.Pairing.ID || rotated.Token == "" || rotated.Token == created.Token {
		t.Fatalf("unexpected rotated pairing: %+v", rotated)
	}
	if _, err := VerificationBridgeIdentify(ctx, created.Token); err == nil {
		t.Fatal("old pairing token remained valid after rotation")
	}
	if _, err := VerificationBridgeIdentify(ctx, rotated.Token); err != nil {
		t.Fatalf("rotated pairing token is invalid: %v", err)
	}
}

func TestVerificationTaskBrowserReadyCompletesWithoutCookie(t *testing.T) {
	ctx := setupBackupTestDB(t)
	_, account := createVerificationFixture(t, ctx)
	created, err := VerificationSessionCreate(ctx, VerificationSessionCreateRequest{
		SiteAccountID: account.ID,
		Operation:     model.SiteOperationCheckin,
	})
	if err != nil {
		t.Fatalf("create verification session: %v", err)
	}
	pairing, err := VerificationBridgePairingCreate(ctx, "Chrome", 30, account.ID)
	if err != nil {
		t.Fatalf("create pairing: %v", err)
	}
	claimed, err := VerificationTaskClaim(ctx, pairing.Token)
	if err != nil {
		t.Fatalf("claim verification task: %v", err)
	}

	ready, err := VerificationTaskBrowserReady(
		ctx,
		pairing.Token,
		claimed.TaskToken,
		"Browser-Test-UA",
	)
	if err != nil {
		t.Fatalf("mark browser verification ready: %v", err)
	}
	if ready.Session.Status != model.VerificationSessionCompleted ||
		ready.Session.Source != "browser" ||
		ready.Session.CookieEncrypted != "" ||
		ready.Session.UserAgent != "Browser-Test-UA" {
		t.Fatalf("unexpected browser session: %+v", ready.Session)
	}
	if ready.Task.Status != model.VerificationTaskCompleted ||
		ready.Task.PairingID == nil ||
		*ready.Task.PairingID != pairing.Pairing.ID ||
		ready.Task.RetryStatus != model.VerificationRetryPending {
		t.Fatalf("unexpected browser task: %+v", ready.Task)
	}

	var reloaded model.SiteAccount
	if err := dbpkg.GetDB().WithContext(ctx).First(&reloaded, account.ID).Error; err != nil {
		t.Fatalf("reload account: %v", err)
	}
	if reloaded.VerificationSessionFenceID != created.Session.ID ||
		reloaded.VerificationCookieEncrypted != "" ||
		reloaded.VerificationExpiresAt != nil {
		t.Fatalf("browser completion persisted replay credentials: %+v", reloaded)
	}
	if _, err := VerificationTaskBrowserReady(
		ctx,
		pairing.Token,
		claimed.TaskToken,
		"Browser-Test-UA",
	); err == nil {
		t.Fatal("browser-ready task accepted duplicate completion")
	}
}

func TestVerificationBrowserBrokerRoundTripIsPairingAndOriginBound(t *testing.T) {
	broker := newVerificationBrowserBroker()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	responseCh := make(chan *VerificationBrowserResponse, 1)
	errorCh := make(chan error, 1)
	go func() {
		response, err := broker.request(ctx, VerificationBrowserRequestInput{
			Binding: VerificationBrowserBinding{
				PairingID: 7,
				TaskID:    8,
				SessionID: 9,
				TargetURL: "https://api.example.com",
			},
			Method:  "POST",
			URL:     "https://api.example.com/api/user/checkin",
			Headers: map[string]string{"Authorization": "Bearer token"},
			Body:    `{"ok":true}`,
		})
		if err != nil {
			errorCh <- err
			return
		}
		responseCh <- response
	}()

	claimed, err := broker.claim(ctx, 7)
	if err != nil {
		t.Fatalf("claim browser request: %v", err)
	}
	if claimed == nil || claimed.RequestToken == "" ||
		claimed.URL != "https://api.example.com/api/user/checkin" {
		t.Fatalf("unexpected claimed request: %+v", claimed)
	}
	if err := broker.complete(6, VerificationBrowserRequestCompletion{
		RequestID:    claimed.RequestID,
		RequestToken: claimed.RequestToken,
		Status:       200,
		Body:         `{"success":true}`,
	}); err == nil {
		t.Fatal("cross-pairing browser response was accepted")
	}
	if err := broker.complete(7, VerificationBrowserRequestCompletion{
		RequestID:    claimed.RequestID,
		RequestToken: claimed.RequestToken,
		Status:       200,
		Headers:      map[string]string{"content-type": "application/json"},
		Body:         `{"success":true}`,
		FinalURL:     "https://api.example.com/api/user/checkin",
	}); err != nil {
		t.Fatalf("complete browser request: %v", err)
	}
	select {
	case err := <-errorCh:
		t.Fatalf("browser request failed: %v", err)
	case response := <-responseCh:
		if response.Status != 200 || response.Body != `{"success":true}` {
			t.Fatalf("unexpected browser response: %+v", response)
		}
	case <-ctx.Done():
		t.Fatal("timed out waiting for browser response")
	}
	if err := broker.complete(7, VerificationBrowserRequestCompletion{
		RequestID:    claimed.RequestID,
		RequestToken: claimed.RequestToken,
		Status:       200,
		FinalURL:     "https://api.example.com/api/user/checkin",
	}); err == nil {
		t.Fatal("browser request token was reusable")
	}
}

func TestVerificationBrowserBrokerRejectsCrossOriginAndOversizeResponse(t *testing.T) {
	broker := newVerificationBrowserBroker()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	if _, err := broker.request(ctx, VerificationBrowserRequestInput{
		Binding: VerificationBrowserBinding{
			PairingID: 1,
			TaskID:    2,
			SessionID: 3,
			TargetURL: "https://api.example.com",
		},
		Method: "GET",
		URL:    "https://evil.example.net/api/user",
	}); err == nil {
		t.Fatal("cross-origin browser request was accepted")
	}

	errorCh := make(chan error, 1)
	go func() {
		_, err := broker.request(ctx, VerificationBrowserRequestInput{
			Binding: VerificationBrowserBinding{
				PairingID: 1,
				TaskID:    2,
				SessionID: 3,
				TargetURL: "https://api.example.com",
			},
			Method: "GET",
			URL:    "https://api.example.com/api/user",
		})
		errorCh <- err
	}()
	claimed, err := broker.claim(ctx, 1)
	if err != nil {
		t.Fatalf("claim browser request: %v", err)
	}
	if err := broker.complete(1, VerificationBrowserRequestCompletion{
		RequestID:    claimed.RequestID,
		RequestToken: claimed.RequestToken,
		Status:       200,
		Body:         strings.Repeat("x", verificationBrowserResponseMaxBytes+1),
	}); err == nil {
		t.Fatal("oversize browser response was accepted")
	}
	cancel()
	<-errorCh
}

func TestVerificationBrowserBrokerRequiresFinalURLAndNormalizesDefaultPorts(t *testing.T) {
	broker := newVerificationBrowserBroker()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	errorCh := make(chan error, 1)
	go func() {
		_, err := broker.request(ctx, VerificationBrowserRequestInput{
			Binding: VerificationBrowserBinding{
				PairingID: 1,
				TaskID:    2,
				SessionID: 3,
				TargetURL: "https://api.example.com:443",
			},
			Method: "GET",
			URL:    "https://api.example.com/api/user",
		})
		errorCh <- err
	}()
	claimed, err := broker.claim(ctx, 1)
	if err != nil {
		t.Fatalf("claim browser request: %v", err)
	}
	if err := broker.complete(1, VerificationBrowserRequestCompletion{
		RequestID:    claimed.RequestID,
		RequestToken: claimed.RequestToken,
		Status:       200,
	}); err == nil {
		t.Fatal("browser response without final url was accepted")
	}
	if err := broker.complete(1, VerificationBrowserRequestCompletion{
		RequestID:    claimed.RequestID,
		RequestToken: claimed.RequestToken,
		Status:       200,
		FinalURL:     "https://api.example.com/api/user",
	}); err != nil {
		t.Fatalf("equivalent default-port origin was rejected: %v", err)
	}
	if err := <-errorCh; err != nil {
		t.Fatalf("browser request failed: %v", err)
	}
}

func TestVerificationBrowserBrokerRejectsForbiddenHeadersAndCancelsSession(t *testing.T) {
	broker := newVerificationBrowserBroker()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	if _, err := broker.request(ctx, VerificationBrowserRequestInput{
		Binding: VerificationBrowserBinding{
			PairingID: 1,
			TaskID:    2,
			SessionID: 3,
			TargetURL: "https://api.example.com",
		},
		Method:  "GET",
		URL:     "https://api.example.com/api/user",
		Headers: map[string]string{"Transfer-Encoding": "chunked"},
	}); err == nil {
		t.Fatal("forbidden browser request header was accepted")
	}

	errorCh := make(chan error, 1)
	go func() {
		_, err := broker.request(ctx, VerificationBrowserRequestInput{
			Binding: VerificationBrowserBinding{
				PairingID: 1,
				TaskID:    2,
				SessionID: 3,
				TargetURL: "https://api.example.com",
			},
			Method: "GET",
			URL:    "https://api.example.com/api/user",
		})
		errorCh <- err
	}()
	if _, err := broker.claim(ctx, 1); err != nil {
		t.Fatalf("claim browser request: %v", err)
	}
	broker.cancelSession(3, fmt.Errorf("session revoked"))
	if err := <-errorCh; err == nil || !strings.Contains(err.Error(), "session revoked") {
		t.Fatalf("session cancellation did not reach browser request: %v", err)
	}
}
