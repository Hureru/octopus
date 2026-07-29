package handlers

import (
	"bytes"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/bestruirui/octopus/internal/apperror"
	"github.com/gin-gonic/gin"
)

func TestReadImportPayloadEnforcesRawAndMultipartLimits(t *testing.T) {
	const limit = int64(8)

	t.Run("raw within limit", func(t *testing.T) {
		c := newSiteImportTestContext(t, "application/json", []byte("12345678"))
		payload, err := readImportPayloadWithLimit(c, limit)
		if err != nil || string(payload) != "12345678" {
			t.Fatalf("bounded raw payload failed: payload=%q err=%v", payload, err)
		}
	})

	for _, contentLength := range []int64{9, -1} {
		t.Run("raw over limit", func(t *testing.T) {
			c := newSiteImportTestContext(t, "application/json", []byte("123456789"))
			c.Request.ContentLength = contentLength
			if _, err := readImportPayloadWithLimit(c, limit); err == nil ||
				apperror.Status(err) != http.StatusRequestEntityTooLarge {
				t.Fatalf("oversized raw payload was accepted: %v", err)
			}
		})
	}

	t.Run("multipart over limit", func(t *testing.T) {
		var body bytes.Buffer
		writer := multipart.NewWriter(&body)
		part, err := writer.CreateFormFile("file", "import.json")
		if err != nil {
			t.Fatalf("create multipart file: %v", err)
		}
		if _, err := part.Write([]byte("123456789")); err != nil {
			t.Fatalf("write multipart file: %v", err)
		}
		if err := writer.Close(); err != nil {
			t.Fatalf("close multipart body: %v", err)
		}
		c := newSiteImportTestContext(t, writer.FormDataContentType(), body.Bytes())
		if _, err := readImportPayloadWithLimit(c, limit); err == nil ||
			apperror.Status(err) != http.StatusRequestEntityTooLarge {
			t.Fatalf("oversized multipart payload was accepted: %v", err)
		}
	})
}

func newSiteImportTestContext(t *testing.T, contentType string, payload []byte) *gin.Context {
	t.Helper()
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/v1/site/import/all-api-hub", bytes.NewReader(payload))
	c.Request.Header.Set("Content-Type", contentType)
	return c
}
