package op

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	dbpkg "github.com/bestruirui/octopus/internal/db"
	"github.com/bestruirui/octopus/internal/model"
)

func TestWSResponseAffinityCleanup(t *testing.T) {
	if dbpkg.GetDB() != nil {
		_ = dbpkg.Close()
	}
	dbPath := filepath.Join(t.TempDir(), "octopus-ws-affinity-cleanup-test.db")
	if err := dbpkg.InitDB("sqlite", dbPath, false); err != nil {
		t.Fatalf("InitDB failed: %v", err)
	}
	t.Cleanup(func() { _ = dbpkg.Close() })

	now := time.Now()
	records := []model.WSResponseAffinity{
		{APIKeyID: 1, GroupID: 1, RequestModel: "m", ResponseIDHash: "expired", ChannelID: 1, ChannelKeyID: 1, ExpiresAt: now.Add(-time.Second)},
		{APIKeyID: 1, GroupID: 1, RequestModel: "m", ResponseIDHash: "live", ChannelID: 1, ChannelKeyID: 1, ExpiresAt: now.Add(time.Hour)},
	}
	if err := dbpkg.GetDB().Create(&records).Error; err != nil {
		t.Fatalf("create affinity rows failed: %v", err)
	}
	deleted, err := WSResponseAffinityCleanup(context.Background(), now)
	if err != nil {
		t.Fatalf("cleanup failed: %v", err)
	}
	if deleted != 1 {
		t.Fatalf("expected one expired row deleted, got %d", deleted)
	}
	var count int64
	if err := dbpkg.GetDB().Model(&model.WSResponseAffinity{}).Count(&count).Error; err != nil {
		t.Fatalf("count failed: %v", err)
	}
	if count != 1 {
		t.Fatalf("expected one live row to remain, got %d", count)
	}
}
