package helper

import (
	"context"
	"path/filepath"
	"testing"

	dbpkg "github.com/bestruirui/octopus/internal/db"
	"github.com/bestruirui/octopus/internal/model"
	"github.com/bestruirui/octopus/internal/op"
	"github.com/bestruirui/octopus/internal/price"
)

func TestLLMPriceRebuild(t *testing.T) {
	if dbpkg.GetDB() != nil {
		_ = dbpkg.Close()
	}
	if err := dbpkg.InitDB("sqlite", filepath.Join(t.TempDir(), "price-rebuild.db"), false); err != nil {
		t.Fatalf("InitDB failed: %v", err)
	}
	ctx := context.Background()
	// 价格缓存是进程级的，不清掉的话 -count=2 第二轮建行会撞上"已存在"
	t.Cleanup(func() {
		_ = op.LLMBatchDelete([]string{"gpt-4o", "gpt-4o-mini", "my-private-model"}, ctx)
		_ = dbpkg.Close()
	})

	preset := price.GetPresetPrice("gpt-4o")
	if preset == nil {
		t.Fatal("test needs gpt-4o in the built-in presets")
	}
	manual := model.LLMPrice{Input: 7, Output: 9}
	rows := []model.LLMInfo{
		{Name: "gpt-4o"},                             // 建行时预设表里还没有，价格停在 0
		{Name: "gpt-4o-mini", LLMPrice: manual},      // 预设表里有，但被手动改过
		{Name: "my-private-model", LLMPrice: manual}, // 预设表里没有，不属于任何渠道
	}
	for _, row := range rows {
		if err := op.LLMCreate(row, ctx); err != nil {
			t.Fatalf("LLMCreate(%s): %v", row.Name, err)
		}
	}

	count, err := LLMPriceRebuild(ctx)
	if err != nil {
		t.Fatalf("LLMPriceRebuild: %v", err)
	}
	if count != 2 {
		t.Fatalf("updated rows = %d, want 2", count)
	}
	if got, _ := op.LLMGet("gpt-4o"); got != *preset {
		t.Errorf("gpt-4o = %+v, want preset %+v", got, *preset)
	}
	if got, _ := op.LLMGet("gpt-4o-mini"); got != *price.GetPresetPrice("gpt-4o-mini") {
		t.Errorf("gpt-4o-mini = %+v, want it reset to the preset", got)
	}
	if got, err := op.LLMGet("my-private-model"); err != nil || got != manual {
		t.Errorf("my-private-model = %+v, %v; want it untouched", got, err)
	}

	// 写进了数据库而不只是缓存。
	var stored model.LLMInfo
	if err := dbpkg.GetDB().First(&stored, "name = ?", "gpt-4o").Error; err != nil || stored.LLMPrice != *preset {
		t.Errorf("stored gpt-4o = %+v, %v; want preset", stored.LLMPrice, err)
	}
	// 再跑一次没有可改的。
	if count, err := LLMPriceRebuild(ctx); err != nil || count != 0 {
		t.Errorf("second rebuild = %d, %v; want 0, nil", count, err)
	}
}
