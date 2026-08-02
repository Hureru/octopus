package modelvendor

import (
	"strings"
	"sync"
)

var (
	indexMu    sync.RWMutex
	modelIndex = map[string]string{}
)

// ReplaceIndex 用外部注册表（models.dev）整体替换「模型名 → 厂商」索引。
//
// 入参是 registry 侧的 provider ID，只接受能通过 prefixAliases 映射到已知厂商的项：
// models.dev 同时收录 openrouter / groq 这类托管方，它们会把同一个模型登记在自己名下，
// 直接采信会让厂商归属随 map 遍历顺序漂移。
func ReplaceIndex(providerByModel map[string]string) {
	next := make(map[string]string, len(providerByModel))
	for modelName, provider := range providerByModel {
		vendor, ok := prefixAliases[strings.ToLower(strings.TrimSpace(provider))]
		if !ok {
			continue
		}
		key := strings.ToLower(strings.TrimSpace(modelName))
		if key == "" {
			continue
		}
		next[key] = vendor
	}

	indexMu.Lock()
	modelIndex = next
	indexMu.Unlock()
}

// IndexSize 返回当前注册表索引条目数，用于确认外部注册表是否已加载。
func IndexSize() int {
	indexMu.RLock()
	defer indexMu.RUnlock()
	return len(modelIndex)
}

func lookupIndex(name string) (string, bool) {
	indexMu.RLock()
	defer indexMu.RUnlock()
	vendor, ok := modelIndex[name]
	return vendor, ok
}
