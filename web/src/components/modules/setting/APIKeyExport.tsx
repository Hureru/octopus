'use client';

import { useId, useMemo, useState } from 'react';
import { useTranslations } from 'next-intl';
import { motion } from 'motion/react';
import { ExternalLink, X } from 'lucide-react';
import { Input } from '@/components/ui/input';
import { Badge } from '@/components/ui/badge';
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select';
import {
    Tabs,
    TabsList,
    TabsTrigger,
    TabsContents,
    TabsContent,
} from '@/components/animate-ui/components/animate/tabs';
import { useGroupList } from '@/api/endpoints/group';
import { CopyIconButton } from '@/components/common/CopyButton';
import { cn } from '@/lib/utils';
import type { APIKey } from '@/api/endpoints/apikey';
import { OverlayPortal } from './OverlayPortal';

type Platform = 'ccswitch' | 'cherrystudio';
type CCSwitchApp = 'claude' | 'codex';
type CherryApiType = 'openai' | 'anthropic';

const NONE = '__none__';

interface CCSwitchForm {
    appType: CCSwitchApp;
    name: string;
    model: string;
    haikuModel: string;
    sonnetModel: string;
    opusModel: string;
}

interface CherryStudioForm {
    name: string;
    apiType: CherryApiType;
}

// 用户填写的 Base URL 可能带尾部斜杠或已含 /v1，统一去掉，避免下游拼出 /v1/v1
function normalizeBaseUrl(url: string): string {
    return url.trim().replace(/\/+$/, '').replace(/\/v1$/i, '');
}

// 协议格式见 cc-switch 的 src-tauri/src/deeplink/parser.rs
export function buildCCSwitchUrl(baseUrl: string, apiKey: string, form: CCSwitchForm): string {
    const base = normalizeBaseUrl(baseUrl);
    const params = new URLSearchParams();
    params.set('resource', 'provider');
    params.set('app', form.appType);
    params.set('name', form.name);
    // Codex 客户端需要带 /v1 的 OpenAI 端点，Claude Code 由客户端自行拼接路径
    params.set('endpoint', form.appType === 'codex' ? `${base}/v1` : base);
    params.set('apiKey', apiKey);
    params.set('model', form.model);
    params.set('homepage', base);
    params.set('enabled', 'true');
    if (form.appType === 'claude') {
        if (form.haikuModel) params.set('haikuModel', form.haikuModel);
        if (form.sonnetModel) params.set('sonnetModel', form.sonnetModel);
        if (form.opusModel) params.set('opusModel', form.opusModel);
    }
    return `ccswitch://v1/import?${params.toString()}`;
}

function base64EncodeUtf8(str: string): string {
    const bytes = new TextEncoder().encode(str);
    let binary = '';
    for (const byte of bytes) {
        binary += String.fromCharCode(byte);
    }
    return btoa(binary);
}

// 协议格式见 cherry-studio 的 src/main/services/protocol/handlers/providersImport.ts；
// 其解码端对 _/- 做了非标准映射，与 URL-safe base64 方向相反，必须用标准 base64 + percent 编码
export function buildCherryStudioUrl(baseUrl: string, apiKey: string, form: CherryStudioForm): string {
    const base = normalizeBaseUrl(baseUrl);
    const id = form.name.toLowerCase().replace(/[^a-z0-9]+/g, '-').replace(/^-+|-+$/g, '') || 'octopus';
    const config = {
        id,
        baseUrl: form.apiType === 'openai' ? `${base}/v1` : base,
        apiKey,
        name: form.name,
        type: form.apiType,
    };
    const data = encodeURIComponent(base64EncodeUtf8(JSON.stringify(config)));
    return `cherrystudio://providers/api-keys?v=1&data=${data}`;
}

// 与 APIKeyForm 中「无限制」「永不过期」开关一致的二选一按钮组
function ToggleGroup<T extends string>({
    value,
    options,
    onChange,
}: {
    value: T;
    options: { value: T; label: string }[];
    onChange: (value: T) => void;
}) {
    return (
        <div className="grid grid-flow-col auto-cols-fr gap-2">
            {options.map((opt) => (
                <button
                    key={opt.value}
                    type="button"
                    onClick={() => onChange(opt.value)}
                    aria-pressed={value === opt.value}
                    className={cn(
                        'h-9 px-3 rounded-xl border text-sm transition-colors',
                        value === opt.value
                            ? 'bg-primary text-primary-foreground border-primary/30'
                            : 'border-border bg-muted/20 text-foreground hover:bg-muted/30'
                    )}
                >
                    {opt.label}
                </button>
            ))}
        </div>
    );
}

function OptionalModelSelect({
    label,
    value,
    options,
    noneLabel,
    onChange,
}: {
    label: string;
    value: string;
    options: string[];
    noneLabel: string;
    onChange: (value: string) => void;
}) {
    return (
        <div className="grid gap-1 text-xs text-muted-foreground">
            {label}
            <Select
                value={value === '' ? NONE : value}
                onValueChange={(v) => onChange(v === NONE ? '' : v)}
            >
                <SelectTrigger className="w-full rounded-xl">
                    <SelectValue />
                </SelectTrigger>
                <SelectContent className="rounded-xl">
                    <SelectItem className="rounded-xl" value={NONE}>{noneLabel}</SelectItem>
                    {options.map((opt) => (
                        <SelectItem className="rounded-xl" key={opt} value={opt}>
                            {opt}
                        </SelectItem>
                    ))}
                </SelectContent>
            </Select>
        </div>
    );
}

export function APIKeyExportOverlay({
    layoutId,
    apiKey,
    baseUrl,
    onClose,
}: {
    layoutId: string;
    apiKey: APIKey;
    baseUrl: string;
    onClose: () => void;
}) {
    const t = useTranslations('setting');
    const { data: groups = [] } = useGroupList();
    const titleId = useId();

    const [platform, setPlatform] = useState<Platform>('ccswitch');
    const [appType, setAppType] = useState<CCSwitchApp>('claude');
    const [model, setModel] = useState('');
    const [customName, setCustomName] = useState<string | null>(null);
    const [haikuModel, setHaikuModel] = useState('');
    const [sonnetModel, setSonnetModel] = useState('');
    const [opusModel, setOpusModel] = useState('');
    const [cherryName, setCherryName] = useState('Octopus');
    const [cherryApiType, setCherryApiType] = useState<CherryApiType>('openai');

    const modelOptions = useMemo(() => {
        const all = Array.from(new Set(groups.map((g) => g.name).filter(Boolean)))
            .sort((a, b) => a.localeCompare(b));
        const supported = apiKey.supported_models?.trim();
        if (!supported) return all;
        const allowed = new Set(supported.split(',').map((m) => m.trim()).filter(Boolean));
        return all.filter((n) => allowed.has(n));
    }, [groups, apiKey.supported_models]);

    // 名称默认跟随所选模型自动生成，用户手动输入后以输入为准，清空输入则恢复自动生成
    const autoName = model ? `octopus_${appType}_${model}` : '';
    const name = customName ?? autoName;

    const ccswitchReady = name.trim() !== '' && model !== '';
    const cherryReady = cherryName.trim() !== '';
    const ready = platform === 'ccswitch' ? ccswitchReady : cherryReady;

    const exportUrl = useMemo(() => {
        if (!ready) return '';
        if (platform === 'ccswitch') {
            return buildCCSwitchUrl(baseUrl, apiKey.api_key, {
                appType,
                name: name.trim(),
                model,
                haikuModel,
                sonnetModel,
                opusModel,
            });
        }
        return buildCherryStudioUrl(baseUrl, apiKey.api_key, {
            name: cherryName.trim(),
            apiType: cherryApiType,
        });
    }, [ready, platform, baseUrl, apiKey.api_key, appType, name, model, haikuModel, sonnetModel, opusModel, cherryName, cherryApiType]);

    const platformLabel = platform === 'ccswitch' ? 'CC Switch' : 'Cherry Studio';

    return (
        <OverlayPortal onClose={onClose}>
            <motion.div
                layoutId={layoutId}
                role="dialog"
                aria-modal="true"
                aria-labelledby={titleId}
                data-slot="dialog-content"
                className="fixed left-1/2 top-1/2 z-50 w-[min(420px,calc(100vw-2rem))] -translate-x-1/2 -translate-y-1/2 bg-card p-5 rounded-3xl border border-border max-h-[80vh] overflow-auto"
                transition={{ type: 'spring', stiffness: 400, damping: 30 }}
            >
                <h3 id={titleId} className="text-sm font-semibold text-card-foreground line-clamp-1 mb-3">
                    {t('apiKey.export.title')} · {apiKey.name}
                </h3>

                <Tabs value={platform} onValueChange={(v) => setPlatform(v as Platform)}>
                    <TabsList className="w-full rounded-xl">
                        <TabsTrigger value="ccswitch">CC Switch</TabsTrigger>
                        <TabsTrigger value="cherrystudio">Cherry Studio</TabsTrigger>
                    </TabsList>
                    {/* TabsContents/TabsContent 原语均自带 overflow-hidden 会裁掉输入框焦点环：
                        外层用内补外负边距留出空间，面板层覆盖为 visible（裁切仍由外层兜底） */}
                    <TabsContents className="p-2 -m-2">
                        <TabsContent value="ccswitch" className="grid gap-2" style={{ overflow: 'visible' }}>
                            <div className="grid gap-1 text-xs text-muted-foreground">
                                {t('apiKey.export.cliTool')}
                                <ToggleGroup
                                    value={appType}
                                    options={[
                                        { value: 'claude', label: 'Claude Code' },
                                        { value: 'codex', label: 'Codex' },
                                    ]}
                                    onChange={setAppType}
                                />
                            </div>

                            <div className="grid gap-1">
                                <div className="text-xs text-muted-foreground">{t('apiKey.export.mainModel')}</div>
                                <div className="max-h-40 overflow-auto rounded-xl p-2">
                                    {modelOptions.length === 0 ? (
                                        <div className="text-xs text-muted-foreground py-2 text-center">
                                            {t('apiKey.form.noModels')}
                                        </div>
                                    ) : (
                                        <div className="flex flex-wrap gap-2">
                                            {modelOptions.map((m) => {
                                                const checked = model === m;
                                                return (
                                                    <button
                                                        key={m}
                                                        type="button"
                                                        onClick={() => setModel(checked ? '' : m)}
                                                        className="text-left"
                                                    >
                                                        <Badge
                                                            variant={checked ? 'default' : 'outline'}
                                                            className={cn(
                                                                'cursor-pointer select-none',
                                                                !checked && 'bg-background/40 hover:bg-background/70'
                                                            )}
                                                        >
                                                            {m}
                                                        </Badge>
                                                    </button>
                                                );
                                            })}
                                        </div>
                                    )}
                                </div>
                            </div>

                            <label className="grid gap-1 text-xs text-muted-foreground">
                                {t('apiKey.export.name')}
                                <Input
                                    value={name}
                                    onChange={(e) => setCustomName(e.target.value === '' ? null : e.target.value)}
                                    placeholder={t('apiKey.export.namePlaceholder')}
                                    className="h-9 text-sm rounded-xl"
                                />
                            </label>

                            {appType === 'claude' && (
                                <>
                                    <OptionalModelSelect
                                        label={t('apiKey.export.haikuModel')}
                                        value={haikuModel}
                                        options={modelOptions}
                                        noneLabel={t('apiKey.export.none')}
                                        onChange={setHaikuModel}
                                    />
                                    <OptionalModelSelect
                                        label={t('apiKey.export.sonnetModel')}
                                        value={sonnetModel}
                                        options={modelOptions}
                                        noneLabel={t('apiKey.export.none')}
                                        onChange={setSonnetModel}
                                    />
                                    <OptionalModelSelect
                                        label={t('apiKey.export.opusModel')}
                                        value={opusModel}
                                        options={modelOptions}
                                        noneLabel={t('apiKey.export.none')}
                                        onChange={setOpusModel}
                                    />
                                </>
                            )}
                        </TabsContent>

                        <TabsContent value="cherrystudio" className="grid gap-2" style={{ overflow: 'visible' }}>
                            <label className="grid gap-1 text-xs text-muted-foreground">
                                {t('apiKey.export.name')}
                                <Input
                                    value={cherryName}
                                    onChange={(e) => setCherryName(e.target.value)}
                                    placeholder={t('apiKey.export.namePlaceholder')}
                                    className="h-9 text-sm rounded-xl"
                                />
                            </label>

                            <div className="grid gap-1 text-xs text-muted-foreground">
                                {t('apiKey.export.apiType')}
                                <ToggleGroup
                                    value={cherryApiType}
                                    options={[
                                        { value: 'openai', label: 'OpenAI' },
                                        { value: 'anthropic', label: 'Anthropic' },
                                    ]}
                                    onChange={setCherryApiType}
                                />
                            </div>
                        </TabsContent>
                    </TabsContents>
                </Tabs>

                <div className="mt-2 text-[11px] text-muted-foreground/80">{t('apiKey.export.hint')}</div>

                <div className="flex gap-2 pt-2 mt-3">
                    <button
                        type="button"
                        onClick={onClose}
                        className="h-9 px-3 flex items-center justify-center gap-1.5 rounded-xl bg-muted text-muted-foreground text-sm font-medium transition-all hover:bg-muted/80 active:scale-[0.98]"
                    >
                        <X className="size-4" />
                        {t('apiKey.form.cancel')}
                    </button>
                    <button
                        type="button"
                        disabled={!ready}
                        onClick={() => window.open(exportUrl, '_blank')}
                        className="flex-1 h-9 flex items-center justify-center gap-1.5 rounded-xl bg-primary text-primary-foreground text-sm font-medium transition-all hover:bg-primary/90 active:scale-[0.98] disabled:opacity-50"
                    >
                        <ExternalLink className="size-4" />
                        {t('apiKey.export.import', { platform: platformLabel })}
                    </button>
                    <CopyIconButton
                        text={exportUrl}
                        className={cn(
                            'size-9 shrink-0 flex items-center justify-center rounded-xl bg-muted text-muted-foreground transition-all hover:bg-muted/80 active:scale-95',
                            !ready && 'opacity-50 pointer-events-none'
                        )}
                        copyIconClassName="size-4"
                        checkIconClassName="size-4"
                    />
                </div>
            </motion.div>
        </OverlayPortal>
    );
}
