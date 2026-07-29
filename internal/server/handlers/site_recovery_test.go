package handlers

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestOptionalPositiveIntQueryRejectsMalformedValues(t *testing.T) {
	for _, target := range []string{
		"/api/v1/site/recovery/verification?account_id=",
		"/api/v1/site/recovery/verification?account_id=0",
		"/api/v1/site/recovery/verification?account_id=-1",
		"/api/v1/site/recovery/verification?account_id=abc",
	} {
		t.Run(target, func(t *testing.T) {
			c, _ := newRecoveryHandlerTestContext(http.MethodGet, target)
			if _, ok := optionalPositiveIntQuery(c, "account_id"); ok {
				t.Fatalf("malformed account_id should be rejected: %s", target)
			}
		})
	}

	c, _ := newRecoveryHandlerTestContext(
		http.MethodGet,
		"/api/v1/site/recovery/verification",
	)
	if value, ok := optionalPositiveIntQuery(c, "account_id"); !ok || value != 0 {
		t.Fatalf("missing optional account_id should list all: value=%d ok=%v", value, ok)
	}
}

func TestListVerificationSessionsRejectsMalformedAccountID(t *testing.T) {
	c, recorder := newRecoveryHandlerTestContext(
		http.MethodGet,
		"/api/v1/site/recovery/verification?account_id=invalid",
	)

	listVerificationSessions(c)

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("unexpected status: got %d want %d", recorder.Code, http.StatusBadRequest)
	}
}

func TestRetryVerificationOperationSchedulesPersistentRetry(t *testing.T) {
	c, recorder := newRecoveryHandlerTestContext(
		http.MethodPost,
		"/api/v1/site/recovery/verification/42/retry",
	)
	c.Params = gin.Params{{Key: "id", Value: "42"}}

	var requeued int64
	var scheduled int64
	retryVerificationOperationWith(
		c,
		func(_ context.Context, sessionID int64) error {
			requeued = sessionID
			return nil
		},
		func(sessionID int64) {
			scheduled = sessionID
		},
	)

	if recorder.Code != http.StatusOK {
		t.Fatalf("unexpected status: got %d want %d body=%s", recorder.Code, http.StatusOK, recorder.Body.String())
	}
	if requeued != 42 || scheduled != 42 {
		t.Fatalf("retry was not requeued and scheduled: requeued=%d scheduled=%d", requeued, scheduled)
	}
	if recorder.Body.String() == "" {
		t.Fatal("retry response body is empty")
	}
}

func newRecoveryHandlerTestContext(
	method string,
	target string,
) (*gin.Context, *httptest.ResponseRecorder) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(method, target, nil)
	return c, recorder
}
