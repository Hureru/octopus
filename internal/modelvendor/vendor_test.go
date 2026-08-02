package modelvendor

import "testing"

func TestDetect(t *testing.T) {
	cases := []struct {
		name  string
		model string
		want  string
	}{
		{"vendor prefix", "z-ai/glm-5.2", VendorZhipuAI},
		{"vendor prefix upper case", "Anthropic/Claude-Opus-4-5", VendorAnthropic},
		{"multi segment keeps known vendor", "openrouter/meta-llama/llama-4-scout", VendorMeta},
		{"unknown host prefix falls back to name", "openrouter/claude-opus-4-5", VendorAnthropic},
		{"bare glm", "glm-4.6", VendorZhipuAI},
		{"claude", "claude-opus-4-5", VendorAnthropic},
		{"gpt", "gpt-5.1", VendorOpenAI},
		{"openai reasoning series", "o3-mini", VendorOpenAI},
		{"gemini", "gemini-3-pro-preview", VendorGoogle},
		{"qwen without separator", "qwen3-max", VendorAlibaba},
		{"deepseek", "deepseek-r1", VendorDeepSeek},
		{"grok", "grok-4", VendorXAI},
		{"kimi", "kimi-k2-0905", VendorMoonshotAI},
		{"doubao", "doubao-seed-1.6", VendorByteDance},
		{"short token needs boundary", "yi-lightning", Vendor01AI},
		{"short token rejects letter suffix", "yielding-model", ""},
		{"xiaomi vendor prefix", "xiaomi/mimo-v2.5-pro", VendorXiaomi},
		{"xiaomi mimo model", "mimo-v2-omni", VendorXiaomi},
		{"mimo rejects letter suffix", "mimosa-1", ""},
		{"baai nested vendor prefix", "Pro/BAAI/bge-reranker-v2-m3", VendorBAAI},
		{"baai bge model", "bge-m3", VendorBAAI},
		{"jina model", "jina-embeddings-v4", VendorJinaAI},
		{"voyage model", "voyage-code-3", VendorVoyageAI},
		{"internvl model", "internvl3.5-241b-a28b", VendorInternLM},
		{"intern s model", "intern-s1-pro", VendorInternLM},
		{"sensetime model", "sensenova-u1-fast", VendorSenseTime},
		{"inclusion ai model", "ling-3.0-flash", VendorInclusionAI},
		{"ling rejects letter suffix", "linguist-model", ""},
		{"nomic model", "nomic-embed-code", VendorNomicAI},
		{"zero entropy embedding", "zembed-1", VendorZeroEntropy},
		{"zero entropy reranker", "zerank-2", VendorZeroEntropy},
		{"poolside model", "laguna-s-2.1", VendorPoolside},
		{"tencent hy3", "hy3", VendorTencent},
		{"cohere north", "north-mini-code", VendorCohere},
		{"cohere embed", "embed-v4.0", VendorCohere},
		{"cohere rerank", "rerank-v4.0-fast", VendorCohere},
		{"generic rerank stays unknown", "rerank-2.5", ""},
		{"minimax speech", "speech-02-hd", VendorMiniMax},
		{"google nano banana", "nano-banana-2", VendorGoogle},
		{"google diffusion gemma", "diffusiongemma-26b-a4b", VendorGoogle},
		{"unknown", "some-random-model", ""},
		{"empty", "   ", ""},
	}

	for _, item := range cases {
		t.Run(item.name, func(t *testing.T) {
			if got := Detect(item.model); got != item.want {
				t.Fatalf("Detect(%q) = %q, want %q", item.model, got, item.want)
			}
		})
	}
}

func TestReplaceIndexEnrichesUnknownModels(t *testing.T) {
	t.Cleanup(func() { ReplaceIndex(nil) })

	ReplaceIndex(map[string]string{
		"mystery-1":       "moonshotai",
		"hosted-only":     "openrouter",
		"  Spaced-2  ":    "  DeepSeek  ",
		"":                "openai",
		"another-mystery": "not-a-known-provider",
	})

	if size := IndexSize(); size != 2 {
		t.Fatalf("IndexSize() = %d, want 2 (unknown providers must be dropped)", size)
	}
	if got := Detect("mystery-1"); got != VendorMoonshotAI {
		t.Fatalf("Detect(mystery-1) = %q, want %q", got, VendorMoonshotAI)
	}
	if got := Detect("spaced-2"); got != VendorDeepSeek {
		t.Fatalf("Detect(spaced-2) = %q, want %q", got, VendorDeepSeek)
	}
	if got := Detect("hosted-only"); got != "" {
		t.Fatalf("Detect(hosted-only) = %q, want empty (hosting provider must not win)", got)
	}
	if got := Detect("another-mystery"); got != "" {
		t.Fatalf("Detect(another-mystery) = %q, want empty", got)
	}
}

func TestDetectPrefersLocalRulesOverIndex(t *testing.T) {
	t.Cleanup(func() { ReplaceIndex(nil) })

	ReplaceIndex(map[string]string{"claude-opus-4-5": "amazon"})
	if got := Detect("claude-opus-4-5"); got != VendorAnthropic {
		t.Fatalf("Detect(claude-opus-4-5) = %q, want %q", got, VendorAnthropic)
	}
}
