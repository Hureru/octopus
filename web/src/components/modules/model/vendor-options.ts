/**
 * 厂商展示配置。id 与后端 internal/modelvendor 的厂商常量一一对应，
 * 后端新增厂商而这里没跟上时，回落为中性样式并直接展示原始 id。
 */
export type VendorOption = {
    id: string;
    label: string;
    className: string;
};

const VENDOR_LIST: VendorOption[] = [
    { id: 'openai', label: 'OpenAI', className: 'bg-emerald-500/10 text-emerald-600 dark:text-emerald-400' },
    { id: 'anthropic', label: 'Anthropic', className: 'bg-orange-500/10 text-orange-600 dark:text-orange-400' },
    { id: 'google', label: 'Google', className: 'bg-blue-500/10 text-blue-600 dark:text-blue-400' },
    { id: 'deepseek', label: 'DeepSeek', className: 'bg-indigo-500/10 text-indigo-600 dark:text-indigo-400' },
    { id: 'xai', label: 'xAI', className: 'bg-neutral-500/10 text-neutral-600 dark:text-neutral-300' },
    { id: 'alibaba', label: 'Qwen', className: 'bg-violet-500/10 text-violet-600 dark:text-violet-400' },
    { id: 'zhipuai', label: 'GLM', className: 'bg-sky-500/10 text-sky-600 dark:text-sky-400' },
    { id: 'minimax', label: 'MiniMax', className: 'bg-rose-500/10 text-rose-600 dark:text-rose-400' },
    { id: 'moonshotai', label: 'Kimi', className: 'bg-fuchsia-500/10 text-fuchsia-600 dark:text-fuchsia-400' },
    { id: 'meta', label: 'Meta', className: 'bg-blue-500/10 text-blue-600 dark:text-blue-400' },
    { id: 'mistral', label: 'Mistral', className: 'bg-amber-500/10 text-amber-600 dark:text-amber-400' },
    { id: 'bytedance', label: 'ByteDance', className: 'bg-cyan-500/10 text-cyan-600 dark:text-cyan-400' },
    { id: 'baidu', label: 'Baidu', className: 'bg-red-500/10 text-red-600 dark:text-red-400' },
    { id: 'tencent', label: 'Tencent', className: 'bg-teal-500/10 text-teal-600 dark:text-teal-400' },
    { id: 'stepfun', label: 'StepFun', className: 'bg-lime-500/10 text-lime-600 dark:text-lime-400' },
    { id: '01ai', label: '01.AI', className: 'bg-green-500/10 text-green-600 dark:text-green-400' },
    { id: 'cohere', label: 'Cohere', className: 'bg-purple-500/10 text-purple-600 dark:text-purple-400' },
    { id: 'perplexity', label: 'Perplexity', className: 'bg-slate-500/10 text-slate-600 dark:text-slate-300' },
    { id: 'amazon', label: 'Amazon', className: 'bg-yellow-500/10 text-yellow-700 dark:text-yellow-400' },
    { id: 'microsoft', label: 'Microsoft', className: 'bg-blue-500/10 text-blue-600 dark:text-blue-400' },
    { id: 'nvidia', label: 'NVIDIA', className: 'bg-lime-500/10 text-lime-600 dark:text-lime-400' },
    { id: 'v0', label: 'v0', className: 'bg-neutral-500/10 text-neutral-600 dark:text-neutral-300' },
];

const VENDOR_BY_ID = new Map(VENDOR_LIST.map((item) => [item.id, item]));

const UNKNOWN_VENDOR_CLASS = 'bg-muted text-muted-foreground';

export function vendorOption(vendor: string): VendorOption | null {
    const id = vendor.trim().toLowerCase();
    if (!id) return null;
    return VENDOR_BY_ID.get(id) ?? { id, label: vendor, className: UNKNOWN_VENDOR_CLASS };
}
