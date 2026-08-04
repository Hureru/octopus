package op

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/bestruirui/octopus/internal/db"
	"github.com/bestruirui/octopus/internal/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const (
	siteProxyPreferenceTTL      = 7 * 24 * time.Hour
	siteProxyCooldownBase       = 30 * time.Second
	siteProxyCooldownMax        = 30 * time.Minute
	siteProxyCloudflareCooldown = 30 * time.Minute
)

type SiteProxyPathDescriptor struct {
	SiteID            int
	SiteAccountID     int
	ProxyMode         model.ProxyUsageMode
	ProxyConfigID     int
	ClashControllerID int
	ClashNode         string
}

func (p SiteProxyPathDescriptor) IdentityKey() string {
	raw := fmt.Sprintf(
		"site=%d|account=%d|mode=%s|proxy=%d|controller=%d|node=%s",
		p.SiteID,
		p.SiteAccountID,
		p.ProxyMode,
		p.ProxyConfigID,
		p.ClashControllerID,
		strings.TrimSpace(p.ClashNode),
	)
	sum := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(sum[:])
}

func SiteProxyPreferenceList(
	ctx context.Context,
	siteID int,
	accountID int,
) ([]model.SiteProxyPreference, error) {
	query := db.GetDB().WithContext(ctx).Model(&model.SiteProxyPreference{})
	if siteID > 0 {
		query = query.Where("site_id = ?", siteID)
	}
	if accountID > 0 {
		query = query.Where("site_account_id IN ?", []int{0, accountID})
	}
	var items []model.SiteProxyPreference
	if err := query.Order("site_account_id DESC, last_success_at DESC, id ASC").Find(&items).Error; err != nil {
		return nil, err
	}
	now := time.Now()
	for index := range items {
		if items[index].ExpiresAt != nil && now.After(*items[index].ExpiresAt) &&
			items[index].Status != model.SiteProxyPreferenceDisabled {
			items[index].Status = model.SiteProxyPreferenceStale
		}
	}
	return items, nil
}

func SiteProxyPreferenceRecordSuccess(
	ctx context.Context,
	path SiteProxyPathDescriptor,
	latency time.Duration,
) error {
	return updateSiteProxyPreference(ctx, path, true, "", latency)
}

func SiteProxyPreferenceRecordFailure(
	ctx context.Context,
	path SiteProxyPathDescriptor,
	failureClass string,
	latency time.Duration,
) error {
	return updateSiteProxyPreference(ctx, path, false, failureClass, latency)
}

func updateSiteProxyPreference(
	ctx context.Context,
	path SiteProxyPathDescriptor,
	success bool,
	failureClass string,
	latency time.Duration,
) error {
	if path.SiteID <= 0 {
		return fmt.Errorf("site id is required")
	}
	identityKey := path.IdentityKey()
	base := model.SiteProxyPreference{
		IdentityKey:       identityKey,
		SiteID:            path.SiteID,
		SiteAccountID:     path.SiteAccountID,
		ProxyMode:         path.ProxyMode,
		ProxyConfigID:     path.ProxyConfigID,
		ClashControllerID: path.ClashControllerID,
		ClashNode:         strings.TrimSpace(path.ClashNode),
		Status:            model.SiteProxyPreferenceHealthy,
	}
	if err := db.GetDB().WithContext(ctx).Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "identity_key"}},
		DoNothing: true,
	}).Create(&base).Error; err != nil {
		return err
	}
	return db.GetDB().WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var item model.SiteProxyPreference
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("identity_key = ?", identityKey).
			First(&item).Error; err != nil {
			return err
		}

		now := time.Now()
		expiresAt := now.Add(siteProxyPreferenceTTL)
		item.ExpiresAt = &expiresAt
		if success {
			item.SuccessCount++
			item.ConsecutiveFailures = 0
			item.Status = model.SiteProxyPreferenceHealthy
			item.CooldownUntil = nil
			item.LastSuccessAt = &now
			if latency > 0 {
				sample := float64(latency.Milliseconds())
				if item.SuccessCount == 1 || item.AverageLatencyMS <= 0 {
					item.AverageLatencyMS = sample
				} else {
					item.AverageLatencyMS = item.AverageLatencyMS*0.8 + sample*0.2
				}
			}
		} else {
			item.FailureCount++
			item.ConsecutiveFailures++
			item.Status = model.SiteProxyPreferenceCooling
			item.LastFailureAt = &now
			cooldown := siteProxyFailureCooldown(item.ConsecutiveFailures, failureClass)
			cooldownUntil := now.Add(cooldown)
			item.CooldownUntil = &cooldownUntil
		}

		return tx.Save(&item).Error
	})
}

func siteProxyFailureCooldown(consecutiveFailures int, failureClass string) time.Duration {
	if failureClass == "cloudflare" {
		return siteProxyCloudflareCooldown
	}
	if consecutiveFailures < 1 {
		consecutiveFailures = 1
	}
	exponent := min(consecutiveFailures-1, 10)
	multiplier := math.Pow(2, float64(exponent))
	cooldown := time.Duration(float64(siteProxyCooldownBase) * multiplier)
	if cooldown > siteProxyCooldownMax {
		return siteProxyCooldownMax
	}
	return cooldown
}

func SiteProxyPreferenceUsable(item model.SiteProxyPreference, now time.Time) bool {
	if item.Status == model.SiteProxyPreferenceDisabled {
		return false
	}
	if item.ExpiresAt != nil && now.After(*item.ExpiresAt) {
		return false
	}
	if item.CooldownUntil != nil && now.Before(*item.CooldownUntil) {
		return false
	}
	return true
}

func SiteProxyPreferenceClear(ctx context.Context, siteID, accountID int) error {
	if siteID <= 0 {
		return fmt.Errorf("site id is required")
	}
	query := db.GetDB().WithContext(ctx).Where("site_id = ?", siteID)
	if accountID > 0 {
		query = query.Where("site_account_id = ?", accountID)
	} else {
		query = query.Where("site_account_id = 0")
	}
	return query.Delete(&model.SiteProxyPreference{}).Error
}

func SiteProxyPreferenceClearAccount(ctx context.Context, accountID int) error {
	account, err := SiteAccountGet(accountID, ctx)
	if err != nil {
		return fmt.Errorf("site account not found")
	}
	return db.GetDB().WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("site_id = ? AND site_account_id = ?", account.SiteID, account.ID).
			Delete(&model.SiteProxyPreference{}).Error; err != nil {
			return err
		}
		return tx.Model(&model.SiteAccount{}).Where("id = ?", account.ID).Updates(map[string]any{
			"preferred_proxy_config_id": nil,
			"preferred_clash_node":      "",
		}).Error
	})
}

func SiteProxyPreferenceClearSite(ctx context.Context, siteID int) error {
	if _, err := SiteGet(siteID, ctx); err != nil {
		return fmt.Errorf("site not found")
	}
	return db.GetDB().WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("site_id = ? AND site_account_id = 0", siteID).
			Delete(&model.SiteProxyPreference{}).Error; err != nil {
			return err
		}
		return tx.Model(&model.Site{}).Where("id = ?", siteID).Updates(map[string]any{
			"preferred_proxy_config_id": nil,
			"preferred_clash_node":      "",
		}).Error
	})
}
