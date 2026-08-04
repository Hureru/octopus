package globalprice

import (
	"strings"

	"github.com/bestruirui/octopus/internal/model"
)

func Get(modelName string) (model.LLMPrice, bool) {
	priceLock.RLock()
	defer priceLock.RUnlock()
	price, ok := prices[strings.ToLower(strings.TrimSpace(modelName))]
	return price, ok
}

func Replace(updated map[string]model.LLMPrice) {
	replacement := make(map[string]model.LLMPrice, len(updated))
	for name, price := range updated {
		name = strings.ToLower(strings.TrimSpace(name))
		if name != "" {
			replacement[name] = price
		}
	}
	priceLock.Lock()
	defer priceLock.Unlock()
	prices = replacement
}
