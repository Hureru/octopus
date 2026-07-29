package migrate

import (
	"fmt"

	"github.com/bestruirui/octopus/internal/model"
	"gorm.io/gorm"
)

func init() {
	RegisterAfterAutoMigration(Migration{
		Version: 21,
		Up:      migrateVerificationSessionFence,
	})
}

func migrateVerificationSessionFence(conn *gorm.DB) error {
	if conn == nil {
		return fmt.Errorf("db is nil")
	}
	if !conn.Migrator().HasTable(&model.SiteAccount{}) ||
		!conn.Migrator().HasTable(&model.VerificationSession{}) {
		return nil
	}
	if !conn.Migrator().HasColumn(&model.SiteAccount{}, "VerificationSessionFenceID") {
		return fmt.Errorf("verification session fence column is missing after schema migration")
	}

	var accounts []model.SiteAccount
	if err := conn.
		Where("verification_cookie_encrypted <> '' AND verification_session_fence_id = 0").
		Find(&accounts).Error; err != nil {
		return fmt.Errorf("load verification credentials for fence backfill: %w", err)
	}
	return conn.Transaction(func(tx *gorm.DB) error {
		for _, account := range accounts {
			var maxSessionID int64
			if err := tx.Model(&model.VerificationSession{}).
				Where(
					"site_account_id = ? AND status = ? AND cookie_encrypted <> ''",
					account.ID,
					model.VerificationSessionCompleted,
				).
				Select("COALESCE(MAX(id), 0)").
				Scan(&maxSessionID).Error; err != nil {
				return fmt.Errorf(
					"load verification session fence for account %d: %w",
					account.ID,
					err,
				)
			}
			if maxSessionID == 0 {
				continue
			}
			if err := tx.Model(&model.SiteAccount{}).
				Where("id = ? AND verification_session_fence_id = 0", account.ID).
				UpdateColumn("verification_session_fence_id", maxSessionID).Error; err != nil {
				return fmt.Errorf(
					"backfill verification session fence for account %d: %w",
					account.ID,
					err,
				)
			}
		}
		return nil
	})
}
