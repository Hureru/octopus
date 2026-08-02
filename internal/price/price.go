package price

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/bestruirui/octopus/internal/client"
	"github.com/bestruirui/octopus/internal/globalprice"
	"github.com/bestruirui/octopus/internal/model"
	"github.com/bestruirui/octopus/internal/modelvendor"
	"github.com/bestruirui/octopus/internal/op"
	"github.com/bestruirui/octopus/internal/utils/log"
)

const llmPriceUrl = "https://models.dev/api.json"

// registryModel 是 models.dev 中单个模型条目，只取本项目用得到的字段。
type registryModel struct {
	ID   string         `json:"id"`
	Cost model.LLMPrice `json:"cost"`
}

type registryProvider struct {
	Models map[string]registryModel `json:"models"`
}

var Provider = []string{
	"openai",     // GPT 系列
	"anthropic",  // Claude 系列
	"google",     // Gemini 系列
	"deepseek",   // DeepSeek 系列
	"xai",        // Grok 系列
	"alibaba",    // Qwen 系列
	"zhipuai",    // GLM 系列
	"minimax",    // MiniMax 系列
	"moonshotai", // Kimi/Moonshot
	"v0",         // v0 系列
}

var lastUpdateTime time.Time

func UpdateLLMPrice(ctx context.Context) error {
	log.Debugf("update LLM price task started")
	startTime := time.Now()
	defer func() {
		log.Debugf("update LLM price task finished, update time: %s", time.Since(startTime))
	}()
	client, err := client.GetHTTPClientSystemProxy(false)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, llmPriceUrl, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/91.0.4472.124 Safari/537.36")
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("failed to fetch LLM info: %s", resp.Status)
	}
	var rawPrice map[string]registryProvider
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("failed to read response body: %w", err)
	}
	if err := json.Unmarshal(body, &rawPrice); err != nil {
		return fmt.Errorf("failed to parse LLM info: %w", err)
	}
	prices := make(map[string]model.LLMPrice)
	for _, provider := range Provider {
		for _, model := range rawPrice[provider].Models {
			model.ID = strings.ToLower(model.ID)
			prices[model.ID] = model.Cost
		}
	}
	globalprice.Replace(prices)
	modelvendor.ReplaceIndex(vendorIndex(rawPrice))
	lastUpdateTime = time.Now()
	return nil
}

// vendorIndex 把 models.dev 的 provider→models 结构翻成「模型名 → provider」索引。
// 这里刻意不套用 Provider 价格白名单：厂商识别需要更宽的覆盖面，
// 由 modelvendor.ReplaceIndex 负责裁掉 openrouter/groq 这类托管方。
func vendorIndex(raw map[string]registryProvider) map[string]string {
	index := make(map[string]string)
	for provider, entry := range raw {
		for key, item := range entry.Models {
			for _, name := range []string{key, item.ID} {
				name = strings.ToLower(strings.TrimSpace(name))
				if name == "" {
					continue
				}
				index[name] = provider
			}
		}
	}
	return index
}

func GetLastUpdateTime() time.Time {
	return lastUpdateTime
}

func GetLLMPrice(modelName string) *model.LLMPrice {
	modelName = strings.ToLower(modelName)
	price, err := op.LLMGet(modelName)
	if err == nil {
		return &price
	}
	price, ok := globalprice.Get(modelName)
	if !ok {
		return nil
	}
	return &price
}
