'use client';

import { useCallback, useState, type FormEvent } from 'react';
import { useTranslations } from 'next-intl';
import { Plus, X, XIcon } from 'lucide-react';
import {
    Dialog,
    DialogContent,
} from '@/components/ui/dialog';
import { Button } from '@/components/ui/button';
import { Input } from '@/components/ui/input';
import {
    Select,
    SelectContent,
    SelectItem,
    SelectTrigger,
    SelectValue,
} from '@/components/ui/select';
import { Switch } from '@/components/ui/switch';
import {
    Accordion,
    AccordionContent,
    AccordionItem,
    AccordionTrigger,
} from '@/components/ui/accordion';
import { ProxySelector } from '@/components/modules/proxy-pool/ProxySelector';
import { TagInput } from './TagInput';
import { toast } from '@/components/common/Toast';
import { useSettingStore } from '@/stores/setting';
import {
    Site as SiteRecord,
    SitePlatform,
    type CustomHeader,
    type SiteRouteBaseURL,
    useCreateSite,
    useDetectSitePlatform,
    useUpdateSite,
} from '@/api/endpoints/site';
import type { ProxyMode } from '@/api/endpoints/proxy-pool';
import { translateSiteMessage } from './site-message';

type SiteFormState = {
    name: string;
    platform: SitePlatform | '';
    base_url: string;
    enabled: boolean;
    proxy_mode: Exclude<ProxyMode, 'inherit'>;
    proxy_config_id: number | null;
    external_checkin_url: string;
    is_pinned: boolean;
    sort_order: number;
    global_weight: number;
    custom_header: CustomHeader[];
    route_base_urls: SiteRouteBaseURL[];
    tags: string[];
};

const AUTO_DETECT_VALUE = '__auto__';

const ROUTE_BASE_URL_OPTIONS: ReadonlyArray<{ value: string; label: string }> = [
    { value: 'openai_chat', label: 'OpenAI Chat' },
    { value: 'openai_response', label: 'OpenAI Responses' },
    { value: 'anthropic', label: 'Anthropic Messages' },
    { value: 'gemini', label: 'Gemini' },
    { value: 'volcengine', label: 'Volcengine' },
    { value: 'openai_embedding', label: 'OpenAI Embedding' },
];

const PLATFORM_LABELS: Record<SitePlatform, string> = {
    [SitePlatform.NewAPI]: 'New API',
    [SitePlatform.AnyRouter]: 'AnyRouter',
    [SitePlatform.OneAPI]: 'One API',
    [SitePlatform.OneHub]: 'One Hub',
    [SitePlatform.DoneHub]: 'Done Hub',
    [SitePlatform.Sub2API]: 'Sub2API',
    [SitePlatform.OpenAI]: 'OpenAI',
    [SitePlatform.Claude]: 'Claude',
    [SitePlatform.Gemini]: 'Gemini',
};

function createEmptySiteForm(): SiteFormState {
    return {
        name: '',
        platform: '',
        base_url: '',
        enabled: true,
        proxy_mode: 'direct',
        proxy_config_id: null,
        external_checkin_url: '',
        is_pinned: false,
        sort_order: 0,
        global_weight: 1,
        custom_header: [{ header_key: '', header_value: '' }],
        route_base_urls: [],
        tags: [],
    };
}

function createSiteForm(site: SiteRecord): SiteFormState {
    return {
        name: site.name,
        platform: site.platform,
        base_url: site.base_url,
        enabled: site.enabled,
        proxy_mode: site.proxy_mode ?? 'direct',
        proxy_config_id: site.proxy_config_id ?? null,
        external_checkin_url: site.external_checkin_url ?? '',
        is_pinned: site.is_pinned,
        sort_order: site.sort_order,
        global_weight: site.global_weight,
        custom_header: site.custom_header.length > 0
            ? site.custom_header.map((item) => ({ ...item }))
            : [{ header_key: '', header_value: '' }],
        route_base_urls: (site.route_base_urls ?? []).map((item) => ({ ...item })),
        tags: [...(site.tags ?? [])],
    };
}

function normalizeSiteRecord(site: SiteRecord): SiteRecord {
    return {
        ...site,
        custom_header: site.custom_header ?? [],
        route_base_urls: site.route_base_urls ?? [],
        tags: site.tags ?? [],
        proxy_mode: site.proxy_mode ?? 'direct',
        proxy_config_id: site.proxy_config_id ?? null,
        external_checkin_url: site.external_checkin_url ?? null,
        is_pinned: site.is_pinned ?? false,
        sort_order: typeof site.sort_order === 'number' ? site.sort_order : 0,
        global_weight:
            typeof site.global_weight === 'number' && site.global_weight > 0
                ? site.global_weight
                : 1,
        accounts: (site.accounts ?? []).map((account) => ({
            ...account,
            proxy_mode: account.proxy_mode ?? 'inherit',
            proxy_config_id: account.proxy_config_id ?? null,
        })),
    };
}

function trimHeaders(items: CustomHeader[]) {
    return items
        .map((item) => ({
            header_key: item.header_key.trim(),
            header_value: item.header_value.trim(),
        }))
        .filter((item) => item.header_key || item.header_value);
}

function trimRouteBaseURLs(items: SiteRouteBaseURL[]) {
    return items
        .map((item) => ({
            route_type: item.route_type.trim(),
            base_url: item.base_url.trim().replace(/\/+$/, ''),
        }))
        .filter((item) => item.route_type || item.base_url);
}

function getErrorMessage(error: unknown) {
    if (error instanceof Error) return error.message;
    if (typeof error === 'object' && error !== null && 'message' in error) {
        const message = (error as { message?: unknown }).message;
        if (typeof message === 'string') return message;
    }
    return '操作失败';
}

interface SiteEditDialogProps {
    open: boolean;
    onOpenChange: (open: boolean) => void;
    site: SiteRecord | null;
    onCreated?: (site: SiteRecord) => void;
    allTags?: string[];
}

/**
 * 站点编辑/创建弹窗。视觉风格与 Channel/Group 卡片编辑面板（MorphingDialog）保持一致：
 * bg-card / rounded-3xl / text-2xl 标题 / 自定义 close 按钮 / 整体 flex 布局并对长表单
 * 提供独立滚动区域，避免视口高度较小时底部按钮被裁切。
 */
export function SiteEditDialog({ open, onOpenChange, site, onCreated, allTags }: SiteEditDialogProps) {
    const t = useTranslations();
    const tProxy = useTranslations('proxyPool');
    const locale = useSettingStore((state) => state.locale);
    const createSite = useCreateSite();
    const updateSite = useUpdateSite();
    const detectPlatform = useDetectSitePlatform();
    const [siteForm, setSiteForm] = useState<SiteFormState>(() =>
        site ? createSiteForm(site) : createEmptySiteForm(),
    );

    const handleSubmit = useCallback(
        async (event: FormEvent<HTMLFormElement>) => {
            event.preventDefault();

            if (!siteForm.name.trim()) {
                toast.error('请输入站点名称');
                return;
            }
            if (!siteForm.base_url.trim()) {
                toast.error('请输入站点地址');
                return;
            }

            let platform = siteForm.platform;
            if (!platform && !site) {
                try {
                    const detected = await detectPlatform.mutateAsync(
                        siteForm.base_url.trim(),
                    );
                    platform = detected.platform as SitePlatform;
                    toast.success(
                        `自动检测到平台：${PLATFORM_LABELS[platform] ?? platform}`,
                    );
                } catch {
                    toast.error('无法自动检测平台类型，请手动选择');
                    return;
                }
            }
            if (!platform) {
                toast.error('请选择平台类型');
                return;
            }

            const customHeader = trimHeaders(siteForm.custom_header);
            const invalidHeader = customHeader.find(
                (item) => !item.header_key || !item.header_value,
            );
            if (invalidHeader) {
                toast.error('自定义 Header 的键和值都不能为空');
                return;
            }

            const routeBaseURLs = trimRouteBaseURLs(siteForm.route_base_urls);
            const invalidRouteBaseURL = routeBaseURLs.find(
                (item) => !item.route_type || !item.base_url,
            );
            if (invalidRouteBaseURL) {
                toast.error('协议路径覆盖的类型和地址都不能为空');
                return;
            }
            const routeTypeSet = new Set<string>();
            const duplicateRoute = routeBaseURLs.find((item) => {
                if (routeTypeSet.has(item.route_type)) return true;
                routeTypeSet.add(item.route_type);
                return false;
            });
            if (duplicateRoute) {
                toast.error('同一协议的路径覆盖只能配置一条');
                return;
            }

            if (siteForm.proxy_mode === 'pool' && !siteForm.proxy_config_id) {
                toast.error(tProxy('selectRequired'));
                return;
            }

            const payload = {
                name: siteForm.name.trim(),
                platform: platform as SitePlatform,
                base_url: siteForm.base_url.trim(),
                enabled: siteForm.enabled,
                proxy_mode: siteForm.proxy_mode,
                proxy_config_id:
                    siteForm.proxy_mode === 'pool' ? siteForm.proxy_config_id : null,
                external_checkin_url: siteForm.external_checkin_url.trim() || null,
                is_pinned: siteForm.is_pinned,
                sort_order: siteForm.sort_order,
                global_weight: siteForm.global_weight,
                custom_header: customHeader,
                route_base_urls: routeBaseURLs,
                tags: siteForm.tags,
            };

            try {
                if (site) {
                    await updateSite.mutateAsync({ id: site.id, ...payload });
                    toast.success('站点已更新');
                    onOpenChange(false);
                } else {
                    const createdSite = normalizeSiteRecord(
                        await createSite.mutateAsync(payload),
                    );
                    toast.success('站点已创建');
                    onOpenChange(false);
                    onCreated?.(createdSite);
                }
            } catch (submitError) {
                toast.error(
                    translateSiteMessage(locale, getErrorMessage(submitError), t),
                );
            }
        },
        [
            siteForm,
            site,
            detectPlatform,
            tProxy,
            updateSite,
            createSite,
            onOpenChange,
            onCreated,
            locale,
            t,
        ],
    );

    const isPending = createSite.isPending || updateSite.isPending || detectPlatform.isPending;

    return (
        <Dialog open={open} onOpenChange={onOpenChange}>
            <DialogContent
                showCloseButton={false}
                className="w-screen max-w-full md:max-w-xl bg-card text-card-foreground px-6 py-4 rounded-3xl flex flex-col gap-0 border-0 sm:max-w-xl h-[min(90dvh,52rem)] overflow-hidden"
            >
                <header className="mb-4 flex items-start justify-between gap-4 shrink-0">
                    <div className="min-w-0 flex-1">
                        <h2 className="text-2xl font-bold text-card-foreground truncate">
                            {site ? '编辑站点' : '新增站点'}
                        </h2>
                    </div>
                    <button
                        type="button"
                        onClick={() => onOpenChange(false)}
                        aria-label="关闭"
                        className="p-1 rounded-md text-muted-foreground hover:text-foreground hover:bg-muted transition-colors shrink-0"
                    >
                        <XIcon className="size-5" />
                    </button>
                </header>

                <form className="flex flex-1 min-h-0 flex-col" onSubmit={handleSubmit}>
                    <div className="flex-1 min-h-0 space-y-5 overflow-y-auto px-1">
                        <div className="grid gap-4 md:grid-cols-2">
                            <label className="grid gap-2 text-sm">
                                <span className="font-medium">站点名称</span>
                                <Input
                                    value={siteForm.name}
                                    onChange={(event) =>
                                        setSiteForm((current) => ({
                                            ...current,
                                            name: event.target.value,
                                        }))
                                    }
                                    placeholder="例如：主站 OneAPI"
                                    className="rounded-xl"
                                />
                            </label>

                            <label className="grid gap-2 text-sm">
                                <span className="font-medium">平台类型</span>
                                <Select
                                    value={siteForm.platform || AUTO_DETECT_VALUE}
                                    onValueChange={(value) =>
                                        setSiteForm((current) => ({
                                            ...current,
                                            platform:
                                                value === AUTO_DETECT_VALUE
                                                    ? ''
                                                    : (value as SitePlatform),
                                        }))
                                    }
                                >
                                    <SelectTrigger className="w-full rounded-xl">
                                        <SelectValue placeholder="自动检测" />
                                    </SelectTrigger>
                                    <SelectContent className="rounded-xl">
                                        {!site && (
                                            <SelectItem className="rounded-xl" value={AUTO_DETECT_VALUE}>自动检测</SelectItem>
                                        )}
                                        {Object.entries(PLATFORM_LABELS).map(([value, label]) => (
                                            <SelectItem className="rounded-xl" key={value} value={value}>
                                                {label}
                                            </SelectItem>
                                        ))}
                                    </SelectContent>
                                </Select>
                            </label>
                        </div>

                        <label className="grid gap-2 text-sm">
                            <span className="font-medium">站点地址</span>
                            <Input
                                value={siteForm.base_url}
                                onChange={(event) =>
                                    setSiteForm((current) => ({
                                        ...current,
                                        base_url: event.target.value,
                                    }))
                                }
                                placeholder="https://example.com"
                                className="rounded-xl"
                            />
                        </label>

                        <label className="grid gap-2 text-sm">
                            <span className="font-medium">手动签到 URL</span>
                            <Input
                                value={siteForm.external_checkin_url}
                                onChange={(event) =>
                                    setSiteForm((current) => ({
                                        ...current,
                                        external_checkin_url: event.target.value,
                                    }))
                                }
                                placeholder="可选：例如 https://example.com/signin"
                                className="rounded-xl"
                            />
                            <span className="text-xs text-muted-foreground">
                                配置后可在站点总览中一键打开此页面进行手动签到。
                            </span>
                        </label>

                        <label className="grid gap-2 text-sm">
                            <span className="font-medium">标签</span>
                            <TagInput
                                value={siteForm.tags}
                                onChange={(tags) =>
                                    setSiteForm((current) => ({ ...current, tags }))
                                }
                                suggestions={allTags}
                            />
                            <span className="text-xs text-muted-foreground">
                                可选：为站点打标签，便于在列表中分类筛选。
                            </span>
                        </label>

                        <ProxySelector
                            value={{ proxy_mode: siteForm.proxy_mode, proxy_config_id: siteForm.proxy_config_id }}
                            onChange={(next) => setSiteForm((current) => ({
                                ...current,
                                proxy_mode: next.proxy_mode as Exclude<ProxyMode, 'inherit'>,
                                proxy_config_id: next.proxy_config_id ?? null,
                            }))}
                        />

                        <div className="flex items-center justify-between rounded-xl border border-border/60 bg-muted/20 px-4 py-3">
                            <div>
                                <div className="text-sm font-medium">启用站点</div>
                                <div className="text-xs text-muted-foreground">
                                    停用后不再投影托管渠道
                                </div>
                            </div>
                            <Switch
                                checked={siteForm.enabled}
                                onCheckedChange={(checked) =>
                                    setSiteForm((current) => ({ ...current, enabled: checked }))
                                }
                            />
                        </div>

                        <Accordion type="single" collapsible className="w-full rounded-xl border bg-card">
                            <AccordionItem value="advanced" className="border-none">
                                <AccordionTrigger className="rounded-xl px-4 py-3 text-sm font-medium text-card-foreground transition-colors hover:bg-muted/30 hover:no-underline">
                                    高级设置
                                </AccordionTrigger>
                                <AccordionContent className="space-y-4 border-t px-4 pb-4 pt-4">
                                    <div className="space-y-2">
                                        <div className="flex items-center justify-between">
                                            <label className="text-sm font-medium text-card-foreground">
                                                自定义 Header {siteForm.custom_header.length > 0 ? `(${siteForm.custom_header.length})` : ''}
                                            </label>
                                            <Button
                                                type="button"
                                                variant="ghost"
                                                size="sm"
                                                onClick={() =>
                                                    setSiteForm((current) => ({
                                                        ...current,
                                                        custom_header: [
                                                            ...current.custom_header,
                                                            { header_key: '', header_value: '' },
                                                        ],
                                                    }))
                                                }
                                                className="h-6 px-2 text-xs text-muted-foreground/70 hover:bg-transparent hover:text-muted-foreground"
                                            >
                                                <Plus className="mr-1 h-3 w-3" />
                                                添加
                                            </Button>
                                        </div>
                                        <div className="space-y-2">
                                            {siteForm.custom_header.map((item, index) => (
                                                <div key={`site-hdr-${index}`} className="flex items-center gap-2">
                                                    <Input
                                                        value={item.header_key}
                                                        onChange={(event) =>
                                                            setSiteForm((current) => ({
                                                                ...current,
                                                                custom_header: current.custom_header.map(
                                                                    (header, headerIndex) =>
                                                                        headerIndex === index
                                                                            ? { ...header, header_key: event.target.value }
                                                                            : header,
                                                                ),
                                                            }))
                                                        }
                                                        placeholder="Header Key"
                                                        className="flex-1 rounded-xl"
                                                    />
                                                    <Input
                                                        value={item.header_value}
                                                        onChange={(event) =>
                                                            setSiteForm((current) => ({
                                                                ...current,
                                                                custom_header: current.custom_header.map(
                                                                    (header, headerIndex) =>
                                                                        headerIndex === index
                                                                            ? {
                                                                                  ...header,
                                                                                  header_value: event.target.value,
                                                                              }
                                                                            : header,
                                                                ),
                                                            }))
                                                        }
                                                        placeholder="Header Value"
                                                        className="flex-1 rounded-xl"
                                                    />
                                                    <Button
                                                        type="button"
                                                        variant="ghost"
                                                        size="sm"
                                                        onClick={() =>
                                                            setSiteForm((current) => ({
                                                                ...current,
                                                                custom_header: current.custom_header.filter(
                                                                    (_, headerIndex) => headerIndex !== index,
                                                                ),
                                                            }))
                                                        }
                                                        disabled={siteForm.custom_header.length <= 1}
                                                        className="h-8 w-8 rounded-xl p-0 text-muted-foreground hover:bg-transparent hover:text-destructive disabled:opacity-40"
                                                        title="Remove"
                                                    >
                                                        <X className="h-4 w-4" />
                                                    </Button>
                                                </div>
                                            ))}
                                        </div>
                                    </div>
                                    <div className="space-y-2">
                                        <div className="flex items-center justify-between">
                                            <label className="text-sm font-medium text-card-foreground">
                                                协议路径覆盖 {siteForm.route_base_urls.length > 0 ? `(${siteForm.route_base_urls.length})` : ''}
                                            </label>
                                            <Button
                                                type="button"
                                                variant="ghost"
                                                size="sm"
                                                onClick={() =>
                                                    setSiteForm((current) => ({
                                                        ...current,
                                                        route_base_urls: [
                                                            ...current.route_base_urls,
                                                            { route_type: '', base_url: '' },
                                                        ],
                                                    }))
                                                }
                                                className="h-6 px-2 text-xs text-muted-foreground/70 hover:bg-transparent hover:text-muted-foreground"
                                            >
                                                <Plus className="mr-1 h-3 w-3" />
                                                添加
                                            </Button>
                                        </div>
                                        <p className="text-xs text-muted-foreground/70">
                                            按协议覆盖请求地址，例如 Anthropic 填 https://example.com/anthropic/v1，留空则用站点地址默认推断。
                                        </p>
                                        <div className="space-y-2">
                                            {siteForm.route_base_urls.map((item, index) => (
                                                <div key={`site-route-${index}`} className="flex items-center gap-2">
                                                    <Select
                                                        value={item.route_type || undefined}
                                                        onValueChange={(value) =>
                                                            setSiteForm((current) => ({
                                                                ...current,
                                                                route_base_urls: current.route_base_urls.map(
                                                                    (route, routeIndex) =>
                                                                        routeIndex === index
                                                                            ? { ...route, route_type: value }
                                                                            : route,
                                                                ),
                                                            }))
                                                        }
                                                    >
                                                        <SelectTrigger className="w-40 rounded-xl">
                                                            <SelectValue placeholder="协议类型" />
                                                        </SelectTrigger>
                                                        <SelectContent>
                                                            {ROUTE_BASE_URL_OPTIONS.map((option) => (
                                                                <SelectItem key={option.value} value={option.value}>
                                                                    {option.label}
                                                                </SelectItem>
                                                            ))}
                                                        </SelectContent>
                                                    </Select>
                                                    <Input
                                                        value={item.base_url}
                                                        onChange={(event) =>
                                                            setSiteForm((current) => ({
                                                                ...current,
                                                                route_base_urls: current.route_base_urls.map(
                                                                    (route, routeIndex) =>
                                                                        routeIndex === index
                                                                            ? { ...route, base_url: event.target.value }
                                                                            : route,
                                                                ),
                                                            }))
                                                        }
                                                        placeholder="https://example.com/anthropic/v1"
                                                        className="flex-1 rounded-xl"
                                                    />
                                                    <Button
                                                        type="button"
                                                        variant="ghost"
                                                        size="sm"
                                                        onClick={() =>
                                                            setSiteForm((current) => ({
                                                                ...current,
                                                                route_base_urls: current.route_base_urls.filter(
                                                                    (_, routeIndex) => routeIndex !== index,
                                                                ),
                                                            }))
                                                        }
                                                        className="h-8 w-8 rounded-xl p-0 text-muted-foreground hover:bg-transparent hover:text-destructive disabled:opacity-40"
                                                        title="Remove"
                                                    >
                                                        <X className="h-4 w-4" />
                                                    </Button>
                                                </div>
                                            ))}
                                        </div>
                                    </div>
                                </AccordionContent>
                            </AccordionItem>
                        </Accordion>
                    </div>

                    <footer className="mt-5 flex shrink-0 flex-col gap-3 px-1 pt-2 sm:flex-row">
                        <Button
                            type="button"
                            variant="secondary"
                            className="h-12 w-full rounded-2xl sm:flex-1"
                            onClick={() => onOpenChange(false)}
                        >
                            取消
                        </Button>
                        <Button
                            type="submit"
                            className="h-12 w-full rounded-2xl sm:flex-1"
                            disabled={isPending}
                        >
                            {isPending ? '保存中...' : site ? '保存修改' : '创建站点'}
                        </Button>
                    </footer>
                </form>
            </DialogContent>
        </Dialog>
    );
}
