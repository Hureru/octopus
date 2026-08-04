package globalprice

import (
	"testing"

	"github.com/bestruirui/octopus/internal/model"
)

func TestReplaceRemovesStalePrices(t *testing.T) {
	Replace(map[string]model.LLMPrice{
		"stale-model": {Input: 1},
	})
	Replace(map[string]model.LLMPrice{
		"current-model": {Input: 2},
	})
	if _, ok := Get("stale-model"); ok {
		t.Fatal("replacement retained a stale model price")
	}
	if price, ok := Get("current-model"); !ok || price.Input != 2 {
		t.Fatalf("replacement lost current model price: %+v, %t", price, ok)
	}
}
