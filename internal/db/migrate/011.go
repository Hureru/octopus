package migrate

import "gorm.io/gorm"

func init() {
	// 版本 11 原来建 ws_response_affinities 表。那张表不再使用，这里留成空操作：
	// 全新安装不再建它，已经跑过 11 的库不受影响。
	RegisterAfterAutoMigration(Migration{
		Version: 11,
		Up:      func(*gorm.DB) error { return nil },
	})
}
