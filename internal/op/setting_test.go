package op

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/bestruirui/octopus/internal/model"
)

func TestSettingListRedactsClientSecrets(t *testing.T) {
	ctx := setupBackupTestDB(t)
	if err := settingRefreshCache(ctx); err != nil {
		t.Fatalf("refresh setting cache: %v", err)
	}
	if err := SettingSetString(model.SettingKeyJWTSecret, "jwt-secret-value"); err != nil {
		t.Fatalf("seed jwt secret: %v", err)
	}
	if err := SettingSetString(model.SettingKeyWebDAVPassword, "webdav-secret-value"); err != nil {
		t.Fatalf("seed WebDAV secret: %v", err)
	}

	settings, err := SettingList(context.Background())
	if err != nil {
		t.Fatalf("SettingList failed: %v", err)
	}
	payload, err := json.Marshal(settings)
	if err != nil {
		t.Fatalf("marshal settings: %v", err)
	}
	if strings.Contains(string(payload), "jwt-secret-value") ||
		strings.Contains(string(payload), "webdav-secret-value") {
		t.Fatal("client settings payload contains a secret value")
	}
	for _, setting := range settings {
		if setting.Key == model.SettingKeyJWTSecret {
			t.Fatal("JWT secret key was exposed to the client")
		}
		if setting.Key == model.SettingKeyWebDAVPassword && setting.Value != "" {
			t.Fatal("WebDAV password was not redacted")
		}
	}
}

func TestSettingForClientRedactsMutationResponse(t *testing.T) {
	setting, ok := SettingForClient(model.Setting{
		Key:   model.SettingKeyWebDAVPassword,
		Value: "secret",
	})
	if !ok || setting.Value != "" {
		t.Fatalf("WebDAV mutation response was not redacted: ok=%v", ok)
	}
	if _, ok := SettingForClient(model.Setting{
		Key:   model.SettingKeyJWTSecret,
		Value: "secret",
	}); ok {
		t.Fatal("JWT secret should be omitted from client responses")
	}
}
