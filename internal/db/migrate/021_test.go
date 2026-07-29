package migrate

import (
	"testing"
	"time"

	"github.com/bestruirui/octopus/internal/model"
	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

func TestMigrateVerificationSessionFenceProtectsExistingCredential(t *testing.T) {
	conn, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := conn.AutoMigrate(&model.SiteAccount{}, &model.VerificationSession{}); err != nil {
		t.Fatalf("AutoMigrate: %v", err)
	}
	account := model.SiteAccount{
		ID:                          1,
		SiteID:                      1,
		Name:                        "legacy",
		CredentialType:              model.SiteCredentialTypeAccessToken,
		VerificationCookieEncrypted: "encrypted",
	}
	if err := conn.Create(&account).Error; err != nil {
		t.Fatalf("seed account: %v", err)
	}
	now := time.Now()
	sessions := []model.VerificationSession{
		{
			SiteID:          1,
			SiteAccountID:   account.ID,
			Status:          model.VerificationSessionCompleted,
			CookieEncrypted: "session-cookie",
			ExpiresAt:       now.Add(time.Hour),
		},
		{
			SiteID:        1,
			SiteAccountID: account.ID,
			Status:        model.VerificationSessionPending,
			ExpiresAt:     now.Add(time.Hour),
		},
	}
	if err := conn.Create(&sessions).Error; err != nil {
		t.Fatalf("seed sessions: %v", err)
	}

	if err := migrateVerificationSessionFence(conn); err != nil {
		t.Fatalf("migrateVerificationSessionFence: %v", err)
	}
	var got model.SiteAccount
	if err := conn.First(&got, account.ID).Error; err != nil {
		t.Fatalf("load account: %v", err)
	}
	if got.VerificationSessionFenceID != sessions[0].ID {
		t.Fatalf(
			"verification fence = %d, want %d",
			got.VerificationSessionFenceID,
			sessions[0].ID,
		)
	}
	if err := migrateVerificationSessionFence(conn); err != nil {
		t.Fatalf("migration rerun failed: %v", err)
	}
}
