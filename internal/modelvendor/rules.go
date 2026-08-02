// Package modelvendor 根据模型名推断所属厂商，供模型发现界面做分类与筛选。
//
// 识别顺序：显式的 "vendor/model" 前缀 → 本地命名规则表 → 外部注册表（models.dev）。
// 本包不依赖 op/price，避免与 price 包形成循环引用。
package modelvendor

// 厂商 ID 与 models.dev 的 provider ID 对齐，便于用注册表数据增强本地规则。
const (
	VendorOpenAI     = "openai"
	VendorAnthropic  = "anthropic"
	VendorGoogle     = "google"
	VendorDeepSeek   = "deepseek"
	VendorXAI        = "xai"
	VendorAlibaba    = "alibaba"
	VendorZhipuAI    = "zhipuai"
	VendorMiniMax    = "minimax"
	VendorMoonshotAI = "moonshotai"
	VendorMeta       = "meta"
	VendorMistral    = "mistral"
	VendorByteDance  = "bytedance"
	VendorBaidu      = "baidu"
	VendorTencent    = "tencent"
	VendorStepFun    = "stepfun"
	Vendor01AI       = "01ai"
	VendorCohere     = "cohere"
	VendorPerplexity = "perplexity"
	VendorAmazon     = "amazon"
	VendorMicrosoft  = "microsoft"
	VendorNvidia     = "nvidia"
	VendorV0         = "v0"
)

// shortTokenMaxLen 以内的 token 属于易误伤的短前缀（o3 / yi / phi 等），
// 命中后要求紧随其后的字符不是字母，避免 "yi" 匹配到 "yield-xxx"。
const shortTokenMaxLen = 4

// prefixAliases 归一 "vendor/model" 形式中的厂商段（OpenRouter 风格聚合站常见），
// 同时用于过滤 models.dev 注册表：未收录的段（openrouter/groq 等托管方）不视为厂商。
var prefixAliases = map[string]string{
	"openai":       VendorOpenAI,
	"azure-openai": VendorOpenAI,

	"anthropic": VendorAnthropic,
	"claude":    VendorAnthropic,

	"google":        VendorGoogle,
	"google-vertex": VendorGoogle,
	"googleai":      VendorGoogle,
	"vertex":        VendorGoogle,
	"gemini":        VendorGoogle,

	"deepseek":    VendorDeepSeek,
	"deepseek-ai": VendorDeepSeek,

	"xai":  VendorXAI,
	"x-ai": VendorXAI,
	"grok": VendorXAI,

	"alibaba":      VendorAlibaba,
	"alibabacloud": VendorAlibaba,
	"dashscope":    VendorAlibaba,
	"tongyi":       VendorAlibaba,
	"qwen":         VendorAlibaba,

	"zhipuai":  VendorZhipuAI,
	"zhipu":    VendorZhipuAI,
	"z-ai":     VendorZhipuAI,
	"bigmodel": VendorZhipuAI,
	"thudm":    VendorZhipuAI,
	"glm":      VendorZhipuAI,

	"minimax":    VendorMiniMax,
	"minimaxai":  VendorMiniMax,
	"minimax-ai": VendorMiniMax,

	"moonshotai": VendorMoonshotAI,
	"moonshot":   VendorMoonshotAI,
	"kimi":       VendorMoonshotAI,

	"meta":       VendorMeta,
	"meta-llama": VendorMeta,
	"llama":      VendorMeta,

	"mistral":   VendorMistral,
	"mistralai": VendorMistral,

	"bytedance":  VendorByteDance,
	"volcengine": VendorByteDance,
	"doubao":     VendorByteDance,
	"ark":        VendorByteDance,

	"baidu":  VendorBaidu,
	"ernie":  VendorBaidu,
	"wenxin": VendorBaidu,

	"tencent": VendorTencent,
	"hunyuan": VendorTencent,

	"stepfun": VendorStepFun,
	"step":    VendorStepFun,

	"01ai":  Vendor01AI,
	"01-ai": Vendor01AI,
	"yi":    Vendor01AI,

	"cohere": VendorCohere,

	"perplexity":    VendorPerplexity,
	"perplexity-ai": VendorPerplexity,

	"amazon":         VendorAmazon,
	"amazon-bedrock": VendorAmazon,
	"aws":            VendorAmazon,
	"bedrock":        VendorAmazon,

	"microsoft": VendorMicrosoft,

	"nvidia": VendorNvidia,

	"v0": VendorV0,
}

// namePattern 描述一组共享厂商归属的模型名前缀。
type namePattern struct {
	vendor string
	tokens []string
}

// namePatterns 按顺序匹配去掉厂商段之后的模型名（已转小写）。
// 前缀之间不重叠，顺序仅影响可读性分组。
var namePatterns = []namePattern{
	{VendorAnthropic, []string{"claude"}},
	{VendorOpenAI, []string{
		"gpt", "chatgpt", "codex", "o1", "o3", "o4",
		"text-embedding-3", "text-embedding-ada", "dall-e", "whisper", "sora", "tts-",
	}},
	{VendorGoogle, []string{"gemini", "gemma", "imagen", "veo"}},
	{VendorDeepSeek, []string{"deepseek"}},
	{VendorXAI, []string{"grok"}},
	{VendorAlibaba, []string{"qwen", "qwq", "qvq", "wan", "tongyi"}},
	{VendorZhipuAI, []string{"glm", "chatglm", "codegeex", "cogview", "cogvideo"}},
	{VendorMoonshotAI, []string{"kimi", "moonshot"}},
	{VendorMiniMax, []string{"minimax", "abab"}},
	{VendorMeta, []string{"llama", "codellama"}},
	{VendorMistral, []string{"mistral", "mixtral", "ministral", "magistral", "codestral", "devstral", "pixtral"}},
	{VendorByteDance, []string{"doubao", "seed", "seedream", "seedance", "skylark"}},
	{VendorBaidu, []string{"ernie", "wenxin"}},
	{VendorTencent, []string{"hunyuan"}},
	{VendorStepFun, []string{"step"}},
	{Vendor01AI, []string{"yi"}},
	{VendorCohere, []string{"command", "rerank"}},
	{VendorPerplexity, []string{"sonar"}},
	{VendorAmazon, []string{"nova", "titan"}},
	{VendorMicrosoft, []string{"phi"}},
	{VendorNvidia, []string{"nemotron"}},
	{VendorV0, []string{"v0"}},
}
