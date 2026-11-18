package helper

import (
	"context"

	"github.com/bestruirui/octopus/internal/model"
	"github.com/bestruirui/octopus/internal/op"
	"github.com/bestruirui/octopus/internal/price"
)

func LLMPriceAddToDB(modelNames []string, ctx context.Context) error {
	newLLMInfos := make([]model.LLMInfo, 0, len(modelNames))
	for _, modelName := range modelNames {
		if modelName == "" {
			continue
		}
		modelPrice := price.GetLLMPrice(modelName)
		if modelPrice != nil {
			newLLMInfos = append(newLLMInfos, model.LLMInfo{
				Name:     modelName,
				LLMPrice: *modelPrice,
			})
		} else {
			newLLMInfos = append(newLLMInfos, model.LLMInfo{Name: modelName})
		}
	}
	if len(newLLMInfos) > 0 {
		return op.LLMBatchCreate(newLLMInfos, ctx)
	}
	return nil
}

func LLMPriceDeleteFromDBWithNoPrice(modelNames []string, ctx context.Context) error {
	if len(modelNames) == 0 {
		return nil
	}
	needDeleteModelNames := make([]string, 0, len(modelNames))
	for _, modelName := range modelNames {
		if modelName == "" {
			continue
		}
		modelPrice, err := op.LLMGet(modelName)
		if err != nil {
			return err
		}
		if modelPrice.Input != 0 || modelPrice.Output != 0 || modelPrice.CacheRead != 0 || modelPrice.CacheWrite != 0 {
			continue
		}
		needDeleteModelNames = append(needDeleteModelNames, modelName)
	}
	if len(needDeleteModelNames) > 0 {
		return op.LLMBatchDelete(needDeleteModelNames, ctx)
	}
	return nil
}

// LLMPriceRebuild 用预设表重新匹配数据库里的价格，返回被改动的行数。
// 预设表里有的模型一律改回预设价，手动改过的也一样；预设表里没有的模型保持原样。不删除任何行。
// 用途：价格行在模型首次出现时写入，之后数据库行优先于预设表，所以当时预设表里还没有的模型会一直是 0，
// 预设价更新后旧行也不会跟着变。
func LLMPriceRebuild(ctx context.Context) (int, error) {
	llmInfos, err := op.LLMList(ctx)
	if err != nil {
		return 0, err
	}
	updated := 0
	for _, llmInfo := range llmInfos {
		preset := price.GetPresetPrice(llmInfo.Name)
		if preset == nil || *preset == llmInfo.LLMPrice {
			continue
		}
		llmInfo.LLMPrice = *preset
		if err := op.LLMUpdate(llmInfo, ctx); err != nil {
			return updated, err
		}
		updated++
	}
	return updated, nil
}
