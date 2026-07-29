package migrate

import (
	"fmt"
	"time"

	"github.com/bestruirui/octopus/internal/model"
	"gorm.io/gorm"
)

func init() {
	RegisterAfterAutoMigration(Migration{
		Version: 22,
		Up:      migrateVerificationPairingScope,
	})
}

func migrateVerificationPairingScope(conn *gorm.DB) error {
	if conn == nil {
		return fmt.Errorf("db is nil")
	}
	if !conn.Migrator().HasTable(&model.VerificationBridgePairing{}) {
		return nil
	}
	if !conn.Migrator().HasColumn(&model.VerificationBridgePairing{}, "SiteAccountID") {
		return fmt.Errorf("verification pairing scope column is missing after schema migration")
	}
	now := time.Now()
	return conn.Model(&model.VerificationBridgePairing{}).
		Where("site_account_id = 0 AND revoked_at IS NULL").
		Update("revoked_at", now).Error
}
