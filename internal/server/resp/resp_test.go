package resp

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/bestruirui/octopus/internal/apperror"
	"github.com/gin-gonic/gin"
)

func TestErrorWithAppErrorUsesAppStatusAndParams(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)

	err := apperror.New("site.sync.missing_group_key", "missing key").
		WithStatus(http.StatusBadRequest).
		WithParam("groupKey", "default")

	ErrorWithAppError(ctx, http.StatusInternalServerError, err)

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d", recorder.Code)
	}

	var body ResponseStruct
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode response failed: %v", err)
	}
	if body.Code != http.StatusBadRequest {
		t.Fatalf("body.Code = %d", body.Code)
	}
	if body.ErrorCode != "site.sync.missing_group_key" {
		t.Fatalf("body.ErrorCode = %q", body.ErrorCode)
	}
	if body.Message != "missing key" {
		t.Fatalf("body.Message = %q", body.Message)
	}
	if body.Params["groupKey"] != "default" {
		t.Fatalf("body.Params[groupKey] = %#v", body.Params["groupKey"])
	}
}

func TestErrorWithAppErrorUsesFallbackStatus(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)

	ErrorWithAppError(ctx, http.StatusInternalServerError, apperror.New(apperror.CodeCommonInternalError, "failed"))

	if recorder.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d", recorder.Code)
	}
}
