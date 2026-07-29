package webdav

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/bestruirui/octopus/internal/db"
	"github.com/bestruirui/octopus/internal/model"
	"github.com/bestruirui/octopus/internal/op"
	xwebdav "golang.org/x/net/webdav"
)

func TestRunBackupUploadsLengthBoundedZipAndRestores(t *testing.T) {
	ctx := setupWebDAVBackupTest(t)
	tempRoot := t.TempDir()
	t.Setenv("TMPDIR", tempRoot)

	var uploadedLength atomic.Int64
	server := newMemoryWebDAVServer(t, func(request *http.Request) {
		if request.Method == http.MethodPut {
			uploadedLength.Store(request.ContentLength)
		}
	})
	configureWebDAVTest(t, server.URL)

	rate := model.CurrencyRate{Currency: "EUR", RateToUSD: 1.08}
	if err := db.GetDB().WithContext(ctx).Create(&rate).Error; err != nil {
		t.Fatalf("seed currency rate: %v", err)
	}
	if err := RunBackup(ctx); err != nil {
		t.Fatalf("RunBackup failed: %v", err)
	}
	backups, err := ListBackups(ctx)
	if err != nil {
		t.Fatalf("ListBackups failed: %v", err)
	}
	if len(backups) != 1 || !strings.HasSuffix(backups[0].Name, backupZipSuffix) {
		t.Fatalf("unexpected backup list: %+v", backups)
	}
	if uploadedLength.Load() <= 0 || uploadedLength.Load() != backups[0].Size {
		t.Fatalf("upload did not preserve content length: header=%d file=%d", uploadedLength.Load(), backups[0].Size)
	}

	if err := db.GetDB().WithContext(ctx).Where("currency = ?", "EUR").Delete(&model.CurrencyRate{}).Error; err != nil {
		t.Fatalf("delete seeded rate: %v", err)
	}
	if _, err := RestoreFromBackup(ctx, backups[0].Name); err != nil {
		t.Fatalf("RestoreFromBackup failed: %v", err)
	}
	var restored model.CurrencyRate
	if err := db.GetDB().WithContext(ctx).First(&restored, "currency = ?", "EUR").Error; err != nil {
		t.Fatalf("restored currency rate missing: %v", err)
	}
	if restored.RateToUSD != rate.RateToUSD {
		t.Fatalf("unexpected restored rate: %+v", restored)
	}
	if matches, err := filepath.Glob(filepath.Join(tempRoot, "octopus-webdav-*")); err != nil || len(matches) != 0 {
		t.Fatalf("temporary WebDAV files were not removed: matches=%v err=%v", matches, err)
	}
}

func TestRunBackupRejectsArchiveThatCannotBeRestored(t *testing.T) {
	ctx := setupWebDAVBackupTest(t)
	server := newMemoryWebDAVServer(t, nil)
	configureWebDAVTest(t, server.URL)
	cfg, err := LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig failed: %v", err)
	}
	client, err := NewClient(cfg)
	if err != nil {
		t.Fatalf("NewClient failed: %v", err)
	}
	if err := client.MkdirAll(cfg.BackupPath, 0755); err != nil {
		t.Fatalf("create remote path: %v", err)
	}

	if err := runBackupWithLimit(ctx, 1); err == nil ||
		!strings.Contains(err.Error(), "upload size limit") {
		t.Fatalf("oversized backup was accepted: %v", err)
	}
	backups, err := ListBackups(ctx)
	if err != nil {
		t.Fatalf("ListBackups failed: %v", err)
	}
	if len(backups) != 0 {
		t.Fatalf("oversized backup was uploaded: %+v", backups)
	}
}

func TestRestoreFromLegacyJSONBackup(t *testing.T) {
	ctx := setupWebDAVBackupTest(t)
	server := newMemoryWebDAVServer(t, nil)
	configureWebDAVTest(t, server.URL)

	rate := model.CurrencyRate{Currency: "JPY", RateToUSD: 0.0067}
	if err := db.GetDB().WithContext(ctx).Create(&rate).Error; err != nil {
		t.Fatalf("seed currency rate: %v", err)
	}
	dump, err := op.DBExportAll(ctx, false, false)
	if err != nil {
		t.Fatalf("DBExportAll failed: %v", err)
	}
	payload, err := json.Marshal(dump)
	if err != nil {
		t.Fatalf("marshal legacy dump: %v", err)
	}
	payload = append(payload, '\n', ' ', '\t')
	cfg, err := LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig failed: %v", err)
	}
	client, err := NewClient(cfg)
	if err != nil {
		t.Fatalf("NewClient failed: %v", err)
	}
	if err := client.MkdirAll(cfg.BackupPath, 0755); err != nil {
		t.Fatalf("create remote path: %v", err)
	}
	filename := backupPrefix + "20260728010101" + backupJSONSuffix
	if err := client.Write(path.Join(cfg.BackupPath, filename), payload, 0644); err != nil {
		t.Fatalf("upload legacy backup: %v", err)
	}
	if err := db.GetDB().WithContext(ctx).Where("currency = ?", "JPY").Delete(&model.CurrencyRate{}).Error; err != nil {
		t.Fatalf("delete seeded rate: %v", err)
	}
	if _, err := RestoreFromBackup(ctx, filename); err != nil {
		t.Fatalf("restore legacy JSON: %v", err)
	}
	var restored model.CurrencyRate
	if err := db.GetDB().WithContext(ctx).First(&restored, "currency = ?", "JPY").Error; err != nil {
		t.Fatalf("legacy restored currency rate missing: %v", err)
	}
}

func TestRestoreFromLegacyJSONRejectsTrailingValueBeforeImport(t *testing.T) {
	ctx := setupWebDAVBackupTest(t)
	server := newMemoryWebDAVServer(t, nil)
	configureWebDAVTest(t, server.URL)

	rate := model.CurrencyRate{Currency: "TRAILING", RateToUSD: 0.25}
	if err := db.GetDB().WithContext(ctx).Create(&rate).Error; err != nil {
		t.Fatalf("seed currency rate: %v", err)
	}
	dump, err := op.DBExportAll(ctx, false, false)
	if err != nil {
		t.Fatalf("export legacy dump: %v", err)
	}
	payload, err := json.Marshal(dump)
	if err != nil {
		t.Fatalf("marshal legacy dump: %v", err)
	}
	payload = append(payload, []byte("\n{}")...)

	cfg, err := LoadConfig()
	if err != nil {
		t.Fatalf("load WebDAV config: %v", err)
	}
	client, err := NewClient(cfg)
	if err != nil {
		t.Fatalf("create WebDAV client: %v", err)
	}
	if err := client.MkdirAll(cfg.BackupPath, 0755); err != nil {
		t.Fatalf("create remote path: %v", err)
	}
	filename := backupPrefix + "20260728020202" + backupJSONSuffix
	if err := client.Write(path.Join(cfg.BackupPath, filename), payload, 0644); err != nil {
		t.Fatalf("upload malformed legacy backup: %v", err)
	}
	if err := db.GetDB().WithContext(ctx).Delete(&rate).Error; err != nil {
		t.Fatalf("delete source currency rate: %v", err)
	}

	if _, err := RestoreFromBackup(ctx, filename); err == nil ||
		!strings.Contains(err.Error(), "trailing JSON value") {
		t.Fatalf("trailing JSON value was accepted: %v", err)
	}
	var count int64
	if err := db.GetDB().WithContext(ctx).Model(&model.CurrencyRate{}).
		Where("currency = ?", rate.Currency).
		Count(&count).Error; err != nil {
		t.Fatalf("count currency rates: %v", err)
	}
	if count != 0 {
		t.Fatalf("malformed backup imported rows before rejection: %d", count)
	}
}

func TestBackupFilenameRejectsTraversal(t *testing.T) {
	for _, name := range []string{
		"../octopus-backup-20260728010101.zip",
		"octopus-backup-../secret.zip",
		"octopus-backup-20260728010101.zip/extra",
		`octopus-backup-20260728010101.zip\extra`,
		"other.zip",
	} {
		if isBackupFile(name) {
			t.Fatalf("unsafe backup filename accepted: %q", name)
		}
	}
}

func setupWebDAVBackupTest(t *testing.T) context.Context {
	t.Helper()
	ctx := context.Background()
	if err := db.InitDB("sqlite", filepath.Join(t.TempDir(), "webdav-test.db"), false); err != nil {
		t.Fatalf("InitDB failed: %v", err)
	}
	if err := op.InitCache(); err != nil {
		t.Fatalf("InitCache failed: %v", err)
	}
	sqlDB, err := db.GetDB().DB()
	if err != nil {
		t.Fatalf("open sql DB: %v", err)
	}
	t.Cleanup(func() {
		_ = sqlDB.Close()
	})
	return ctx
}

func configureWebDAVTest(t *testing.T, serverURL string) {
	t.Helper()
	for key, value := range map[model.SettingKey]string{
		model.SettingKeyWebDAVURL:            serverURL,
		model.SettingKeyWebDAVUsername:       "",
		model.SettingKeyWebDAVPassword:       "",
		model.SettingKeyWebDAVBackupPath:     "/backups",
		model.SettingKeyWebDAVRetentionCount: "10",
		model.SettingKeyWebDAVIncludeStats:   "false",
		model.SettingKeyProxyURL:             "",
	} {
		if err := op.SettingSetString(key, value); err != nil {
			t.Fatalf("set %s: %v", key, err)
		}
	}
}

func newMemoryWebDAVServer(t *testing.T, inspect func(*http.Request)) *httptest.Server {
	t.Helper()
	handler := &xwebdav.Handler{
		Prefix:     "/",
		FileSystem: xwebdav.NewMemFS(),
		LockSystem: xwebdav.NewMemLS(),
	}
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if inspect != nil {
			inspect(request)
		}
		handler.ServeHTTP(writer, request)
	}))
	t.Cleanup(server.Close)
	return server
}
