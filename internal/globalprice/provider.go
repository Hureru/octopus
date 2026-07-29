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
	priceLock.Lock()
	defer priceLock.Unlock()
	for name, price := range updated {
		prices[strings.ToLower(strings.TrimSpace(name))] = price
	}
}
