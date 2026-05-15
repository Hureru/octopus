package migrate

import (
	"errors"
	"fmt"
	"strings"

	"github.com/bestruirui/octopus/internal/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

func init() {
	RegisterAfterAutoMigration(Migration{
		Version: 2026051501,
		Up:      migrateProxyPool,
	})
}

type legacyChannelProxyRow struct {
	ID           int
	Proxy        bool
	ChannelProxy *string
}

type legacySiteProxyRow struct {
	ID             int
	Proxy          bool
	SiteProxy      *string
	UseSystemProxy bool
}

type legacySiteAccountProxyRow struct {
	ID           int
	AccountProxy *string
}

func migrateProxyPool(db *gorm.DB) error {
	proxyIDs, err := collectLegacyProxyConfigurations(db)
	if err != nil {
		return err
	}
	if err := migrateChannelProxyFields(db, proxyIDs); err != nil {
		return err
	}
	if err := migrateSiteProxyFields(db, proxyIDs); err != nil {
		return err
	}
	if err := migrateSiteAccountProxyFields(db, proxyIDs); err != nil {
		return err
	}
	return nil
}

func collectLegacyProxyConfigurations(db *gorm.DB) (map[string]int, error) {
	urls := make([]string, 0)
	var channels []legacyChannelProxyRow
	if err := db.Table("channels").Select("id, proxy, channel_proxy").Find(&channels).Error; err != nil {
		return nil, err
	}
	for _, row := range channels {
		if row.ChannelProxy != nil {
			urls = append(urls, *row.ChannelProxy)
		}
	}
	var sites []legacySiteProxyRow
	if err := db.Table("sites").Select("id, proxy, site_proxy, use_system_proxy").Find(&sites).Error; err != nil {
		return nil, err
	}
	for _, row := range sites {
		if row.SiteProxy != nil {
			urls = append(urls, *row.SiteProxy)
		}
	}
	var accounts []legacySiteAccountProxyRow
	if err := db.Table("site_accounts").Select("id, account_proxy").Find(&accounts).Error; err != nil {
		return nil, err
	}
	for _, row := range accounts {
		if row.AccountProxy != nil {
			urls = append(urls, *row.AccountProxy)
		}
	}

	proxyIDs := make(map[string]int)
	for _, raw := range urls {
		normalized, err := model.NormalizeProxyURL(raw)
		if err != nil {
			continue
		}
		if _, exists := proxyIDs[normalized]; exists {
			continue
		}
		var existing model.ProxyConfiguration
		if err := db.Where("url = ?", normalized).First(&existing).Error; err == nil {
			proxyIDs[normalized] = existing.ID
			continue
		} else if !errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, err
		}
		name, err := nextImportedProxyName(db)
		if err != nil {
			return nil, err
		}
		item := model.ProxyConfiguration{
			Name:    name,
			URL:     normalized,
			Enabled: true,
			Remark:  "由历史代理配置迁移生成",
		}
		if err := db.Clauses(clause.OnConflict{DoNothing: true}).Create(&item).Error; err != nil {
			return nil, err
		}
		if item.ID == 0 {
			if err := db.Where("url = ?", normalized).First(&item).Error; err != nil {
				return nil, err
			}
		}
		proxyIDs[normalized] = item.ID
	}
	return proxyIDs, nil
}

func nextImportedProxyName(db *gorm.DB) (string, error) {
	for i := 1; i < 100000; i++ {
		name := fmt.Sprintf("Imported Proxy %d", i)
		var count int64
		if err := db.Model(&model.ProxyConfiguration{}).Where("name = ?", name).Count(&count).Error; err != nil {
			return "", err
		}
		if count == 0 {
			return name, nil
		}
	}
	return "", fmt.Errorf("failed to generate imported proxy name")
}

func migrateChannelProxyFields(db *gorm.DB, proxyIDs map[string]int) error {
	var rows []legacyChannelProxyRow
	if err := db.Table("channels").Select("id, proxy, channel_proxy").Find(&rows).Error; err != nil {
		return err
	}
	for _, row := range rows {
		updates := map[string]any{"proxy_config_id": gorm.Expr("NULL")}
		if !row.Proxy {
			updates["proxy_mode"] = model.ProxyUsageModeDirect
		} else if id, ok := legacyProxyID(row.ChannelProxy, proxyIDs); ok {
			updates["proxy_mode"] = model.ProxyUsageModePool
			updates["proxy_config_id"] = id
		} else {
			updates["proxy_mode"] = model.ProxyUsageModeSystem
		}
		if err := db.Table("channels").Where("id = ?", row.ID).Updates(updates).Error; err != nil {
			return err
		}
	}
	return nil
}

func migrateSiteProxyFields(db *gorm.DB, proxyIDs map[string]int) error {
	var rows []legacySiteProxyRow
	if err := db.Table("sites").Select("id, proxy, site_proxy, use_system_proxy").Find(&rows).Error; err != nil {
		return err
	}
	for _, row := range rows {
		updates := map[string]any{"proxy_config_id": gorm.Expr("NULL")}
		if row.Proxy {
			if id, ok := legacyProxyID(row.SiteProxy, proxyIDs); ok {
				updates["proxy_mode"] = model.ProxyUsageModePool
				updates["proxy_config_id"] = id
			} else {
				updates["proxy_mode"] = model.ProxyUsageModeSystem
			}
		} else if row.UseSystemProxy {
			updates["proxy_mode"] = model.ProxyUsageModeSystem
		} else {
			updates["proxy_mode"] = model.ProxyUsageModeDirect
		}
		if err := db.Table("sites").Where("id = ?", row.ID).Updates(updates).Error; err != nil {
			return err
		}
	}
	return nil
}

func migrateSiteAccountProxyFields(db *gorm.DB, proxyIDs map[string]int) error {
	var rows []legacySiteAccountProxyRow
	if err := db.Table("site_accounts").Select("id, account_proxy").Find(&rows).Error; err != nil {
		return err
	}
	for _, row := range rows {
		updates := map[string]any{"proxy_config_id": gorm.Expr("NULL")}
		if id, ok := legacyProxyID(row.AccountProxy, proxyIDs); ok {
			updates["proxy_mode"] = model.ProxyUsageModePool
			updates["proxy_config_id"] = id
		} else {
			updates["proxy_mode"] = model.ProxyUsageModeInherit
		}
		if err := db.Table("site_accounts").Where("id = ?", row.ID).Updates(updates).Error; err != nil {
			return err
		}
	}
	return nil
}

func legacyProxyID(value *string, proxyIDs map[string]int) (int, bool) {
	if value == nil || strings.TrimSpace(*value) == "" {
		return 0, false
	}
	normalized, err := model.NormalizeProxyURL(*value)
	if err != nil {
		return 0, false
	}
	id, ok := proxyIDs[normalized]
	return id, ok
}
