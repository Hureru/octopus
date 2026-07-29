package migrate

import (
	"testing"
	"time"

	"github.com/bestruirui/octopus/internal/model"
	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

func TestMigrateVerificationPairingScopeRevokesLegacyGlobalPairings(t *testing.T) {
	conn, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := conn.AutoMigrate(&model.VerificationBridgePairing{}); err != nil {
		t.Fatalf("AutoMigrate: %v", err)
	}
	rows := []model.VerificationBridgePairing{
		{Name: "legacy", TokenHash: "legacy", ExpiresAt: time.Now().Add(time.Hour)},
		{Name: "scoped", SiteAccountID: 9, TokenHash: "scoped", ExpiresAt: time.Now().Add(time.Hour)},
	}
	if err := conn.Create(&rows).Error; err != nil {
		t.Fatalf("seed pairings: %v", err)
	}
	if err := migrateVerificationPairingScope(conn); err != nil {
		t.Fatalf("migrateVerificationPairingScope: %v", err)
	}
	var got []model.VerificationBridgePairing
	if err := conn.Order("id ASC").Find(&got).Error; err != nil {
		t.Fatalf("load pairings: %v", err)
	}
	if got[0].RevokedAt == nil {
		t.Fatalf("legacy global pairing was not revoked: %+v", got[0])
	}
	if got[1].RevokedAt != nil {
		t.Fatalf("scoped pairing was revoked: %+v", got[1])
	}
}
