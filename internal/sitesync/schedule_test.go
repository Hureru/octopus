package sitesync

import (
	"testing"
	"time"

	"github.com/bestruirui/octopus/internal/model"
)

func TestBuildNextRandomCheckinAtRespectsRollingInterval(t *testing.T) {
	lastSuccess := time.Date(2026, 3, 24, 15, 0, 0, 0, time.UTC)
	now := time.Date(2026, 3, 24, 16, 0, 0, 0, time.UTC)
	account := &model.SiteAccount{
		Enabled:                    true,
		AutoCheckin:                true,
		RandomCheckin:              true,
		CheckinIntervalHours:       24,
		CheckinRandomWindowMinutes: 120,
		LastCheckinAt:              &lastSuccess,
		LastCheckinStatus:          model.SiteExecutionStatusSuccess,
	}

	next := buildNextRandomCheckinAt(account, now)
	if next == nil {
		t.Fatalf("expected next checkin time")
	}

	earliest := lastSuccess.Add(24 * time.Hour)
	latest := earliest.Add(120 * time.Minute)
	if next.Before(earliest) || next.After(latest) {
		t.Fatalf("expected next checkin between %s and %s, got %s", earliest, latest, next)
	}
}

func TestBuildNextRandomCheckinAtSpreadsOverdueAccountsFromNow(t *testing.T) {
	lastSuccess := time.Date(2026, 3, 20, 10, 0, 0, 0, time.UTC)
	now := time.Date(2026, 3, 24, 16, 0, 0, 0, time.UTC)
	account := &model.SiteAccount{
		Enabled:                    true,
		AutoCheckin:                true,
		RandomCheckin:              true,
		CheckinIntervalHours:       24,
		CheckinRandomWindowMinutes: 60,
		LastCheckinAt:              &lastSuccess,
		LastCheckinStatus:          model.SiteExecutionStatusSuccess,
	}

	next := buildNextRandomCheckinAt(account, now)
	if next == nil {
		t.Fatalf("expected next checkin time")
	}

	latest := now.Add(60 * time.Minute)
	if next.Before(now) || next.After(latest) {
		t.Fatalf("expected overdue account to be rescheduled between %s and %s, got %s", now, latest, next)
	}
}

func TestBuildNextRandomCheckinAtReturnsNilWhenRandomDisabled(t *testing.T) {
	next := buildNextRandomCheckinAt(&model.SiteAccount{
		Enabled:       true,
		AutoCheckin:   true,
		RandomCheckin: false,
	}, time.Now())

	if next != nil {
		t.Fatalf("expected nil next checkin time when random checkin is disabled")
	}
}
