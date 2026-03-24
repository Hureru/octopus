package sitesync

import (
	"context"
	"fmt"
	"math/rand"
	"time"

	"github.com/bestruirui/octopus/internal/db"
	"github.com/bestruirui/octopus/internal/model"
	"github.com/bestruirui/octopus/internal/op"
)

func buildNextRandomCheckinAt(account *model.SiteAccount, now time.Time) *time.Time {
	if account == nil || !account.Enabled || !account.AutoCheckin || !account.RandomCheckin {
		return nil
	}

	intervalHours := account.CheckinIntervalHours
	if intervalHours <= 0 {
		intervalHours = 24
	}
	windowMinutes := account.CheckinRandomWindowMinutes
	if windowMinutes < 0 {
		windowMinutes = 0
	}

	base := now
	if account.LastCheckinAt != nil && !account.LastCheckinAt.IsZero() && account.LastCheckinStatus == model.SiteExecutionStatusSuccess {
		earliest := account.LastCheckinAt.Add(time.Duration(intervalHours) * time.Hour)
		if earliest.After(base) {
			base = earliest
		}
	}

	if windowMinutes > 0 {
		base = base.Add(time.Duration(rand.Intn(windowMinutes+1)) * time.Minute)
	}

	next := base
	return &next
}

func ensureRandomCheckinSchedule(ctx context.Context, account *model.SiteAccount, now time.Time) (*time.Time, error) {
	if account == nil || !account.RandomCheckin {
		return nil, nil
	}
	if account.NextAutoCheckinAt != nil && !account.NextAutoCheckinAt.IsZero() {
		return account.NextAutoCheckinAt, nil
	}

	nextAt := buildNextRandomCheckinAt(account, now)
	if err := persistNextAutoCheckinAt(ctx, account.ID, nextAt); err != nil {
		return nil, err
	}
	account.NextAutoCheckinAt = nextAt
	return nextAt, nil
}

func persistNextAutoCheckinAt(ctx context.Context, accountID int, nextAt *time.Time) error {
	return db.GetDB().WithContext(ctx).
		Model(&model.SiteAccount{}).
		Where("id = ?", accountID).
		Update("next_auto_checkin_at", nextAt).Error
}

func RefreshAccountRandomCheckinSchedule(ctx context.Context, accountID int) error {
	account, err := op.SiteAccountGet(accountID, ctx)
	if err != nil {
		return fmt.Errorf("site account not found")
	}

	nextAt := buildNextRandomCheckinAt(account, time.Now())
	if err := persistNextAutoCheckinAt(ctx, account.ID, nextAt); err != nil {
		return err
	}
	return nil
}
