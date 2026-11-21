package migrate

import (
	"testing"

	"github.com/bestruirui/octopus/internal/model"
	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// legacyWSModeChannel 是升级前的 channels 表里和这次迁移有关的几列。
type legacyWSModeChannel struct {
	ID     int    `gorm:"primaryKey"`
	Name   string `gorm:"unique;not null"`
	WSMode string `gorm:"type:varchar(16);not null;default:'inherit'"`
}

func (legacyWSModeChannel) TableName() string { return "channels" }

// 照着启动时的顺序走一遍升级：先 AutoMigrate（列的默认值变了，SQLite 会重建整张表），再跑迁移。
func TestMigrateChannelWSModeAutoOff(t *testing.T) {
	// 下面有一次预期失败的插入，不让它打日志
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{Logger: logger.Discard})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(&legacyWSModeChannel{}); err != nil {
		t.Fatalf("create the legacy channels table: %v", err)
	}
	want := map[string]model.ChannelWSMode{
		"inherit":     model.ChannelWSModeAuto,
		"passthrough": model.ChannelWSModeAuto,
		"transform":   model.ChannelWSModeAuto,
		"off":         model.ChannelWSModeOff,
	}
	for mode := range want {
		if err := db.Create(&legacyWSModeChannel{Name: mode, WSMode: mode}).Error; err != nil {
			t.Fatalf("insert legacy channel %q: %v", mode, err)
		}
	}
	if err := db.Exec("CREATE TABLE ws_response_affinities (response_id text)").Error; err != nil {
		t.Fatalf("create the legacy affinity table: %v", err)
	}

	if err := db.AutoMigrate(&model.Channel{}); err != nil {
		t.Fatalf("AutoMigrate Channel: %v", err)
	}
	// 跑两遍：迁移要能重复执行
	for range 2 {
		if err := migrateChannelWSModeAutoOff(db); err != nil {
			t.Fatalf("migrateChannelWSModeAutoOff failed: %v", err)
		}
	}

	// 列的默认值也变了：不写 ws_mode 的插入得到 auto
	if err := db.Exec("INSERT INTO channels (name) VALUES ('default')").Error; err != nil {
		t.Fatalf("insert a channel without ws_mode: %v", err)
	}
	want["default"] = model.ChannelWSModeAuto

	var channels []model.Channel
	if err := db.Find(&channels).Error; err != nil {
		t.Fatalf("list channels: %v", err)
	}
	if len(channels) != len(want) {
		t.Fatalf("expected %d channels to survive the upgrade, got %d", len(want), len(channels))
	}
	for _, channel := range channels {
		if channel.WSMode != want[channel.Name] {
			t.Errorf("channel %q: ws_mode = %q, want %q", channel.Name, channel.WSMode, want[channel.Name])
		}
	}
	// 表重建之后，名字的唯一约束还在
	if err := db.Create(&model.Channel{Name: "off"}).Error; err == nil {
		t.Errorf("expected the unique constraint on the channel name to survive the upgrade")
	}
	if db.Migrator().HasTable("ws_response_affinities") {
		t.Errorf("expected ws_response_affinities to be dropped")
	}
}
