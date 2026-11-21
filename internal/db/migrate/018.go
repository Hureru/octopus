package migrate

import (
	"github.com/bestruirui/octopus/internal/model"
	"gorm.io/gorm"
)

func init() {
	RegisterAfterAutoMigration(Migration{
		Version: 18,
		Up:      migrateChannelWSModeAutoOff,
	})
}

func migrateChannelWSModeAutoOff(db *gorm.DB) error {
	// 渠道的“上游 WebSocket”只剩 自动 / 关闭：以前的 inherit、passthrough、transform 都变成自动
	if err := db.Model(&model.Channel{}).
		Where("ws_mode IS NULL OR ws_mode <> ?", string(model.ChannelWSModeOff)).
		Update("ws_mode", string(model.ChannelWSModeAuto)).Error; err != nil {
		return err
	}
	// 续传亲和表已经没人用了，里面只是有效期不超过 1 小时的缓存
	return db.Migrator().DropTable("ws_response_affinities")
}
