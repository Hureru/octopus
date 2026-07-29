package migrate

import (
	"fmt"

	"github.com/bestruirui/octopus/internal/model"
	"gorm.io/gorm"
)

func init() {
	RegisterAfterAutoMigration(Migration{
		Version: 18,
		Up:      migrateRelayLogOutcome,
	})
}

// migrateRelayLogOutcome backfills the compatibility outcome field without
// reinterpreting historical context-canceled records. Those records require
// the explicit, auditable repair API because only a subset has completion
// evidence strong enough to be corrected safely.
func migrateRelayLogOutcome(db *gorm.DB) error {
	if db == nil {
		return fmt.Errorf("db is nil")
	}
	if !db.Migrator().HasTable(&model.RelayLog{}) {
		return nil
	}
	if !hasRelayLogColumn(db, "outcome") {
		return fmt.Errorf("relay_logs outcome column is missing after schema migration")
	}

	if db.Dialector.Name() == "sqlite" {
		for {
			result := db.Exec(`
UPDATE relay_logs
SET outcome = CASE WHEN success = 1 THEN ? ELSE ? END
WHERE id IN (
    SELECT id FROM relay_logs
    WHERE outcome = '' OR outcome IS NULL
    LIMIT ?
)`, model.RequestOutcomeSuccess, model.RequestOutcomeFailed, relayLogSuccessBackfillBatchSize)
			if result.Error != nil {
				return fmt.Errorf("backfill relay_logs outcome: %w", result.Error)
			}
			if result.RowsAffected < int64(relayLogSuccessBackfillBatchSize) {
				return nil
			}
		}
	}

	result := db.Exec(
		"UPDATE relay_logs SET outcome = CASE WHEN success = ? THEN ? ELSE ? END WHERE outcome = '' OR outcome IS NULL",
		true,
		model.RequestOutcomeSuccess,
		model.RequestOutcomeFailed,
	)
	if result.Error != nil {
		return fmt.Errorf("backfill relay_logs outcome: %w", result.Error)
	}
	return nil
}
