'use client';

import { useState, type FormEvent, type ReactNode } from 'react';
import {
    type AllAPIHubImportResult,
    Site as SiteRecord,
    SiteAccount,
    SiteCredentialType,
    SiteOutboundFormatMode,
    type SiteOutboundFormatModeValue,
    SitePlatform,
    type CustomHeader,
    useCheckinAllSites,
    useCheckinSiteAccount,
    useCreateSite,
    useCreateSiteAccount,
    useDeleteSite,
    useDeleteSiteAccount,
    useDetectSitePlatform,
    useEnableSite,
    useEnableSiteAccount,
    useImportAllAPIHub,
    useSiteAvailableModels,
    useSiteBatchAction,
    useSiteDisabledModels,
    useSiteList,
    useSyncAllSites,
    useSyncSiteAccount,
    useUpdateSite,
    useUpdateSiteAccount,
    useUpdateSiteDisabledModels,
} from '@/api/endpoints/site';
import { PageWrapper } from '@/components/common/PageWrapper';
import { toast } from '@/components/common/Toast';
import { Badge } from '@/components/ui/badge';
import { Button } from '@/components/ui/button';
import {
    Dialog,
    DialogContent,
    DialogDescription,
    DialogFooter,
    DialogHeader,
    DialogTitle,
} from '@/components/ui/dialog';
import { Input } from '@/components/ui/input';
import {
    Select,
    SelectContent,
    SelectItem,
    SelectTrigger,
    SelectValue,
} from '@/components/ui/select';
import { Switch } from '@/components/ui/switch';
import { cn } from '@/lib/utils';
import { CheckinPanel } from './CheckinPanel';
import {
    CalendarCheck2,
    Cable,
    CheckSquare,
    CircleAlert,
    FileJson,
    FolderTree,
    Globe2,
    KeyRound,
    Link2,
    Pencil,
    Pin,
    PinOff,
    Plus,
    RefreshCw,
    Search,
    Shield,
    ShieldOff,
    Sparkles,
    Square,
    Trash2,
    TriangleAlert,
    Upload,
    UserRound,
    Wallet,
    Waypoints,
    X,
} from 'lucide-react';

type SiteFormState = {
    name: string;
    platform: SitePlatform | '';
    base_url: string;
    enabled: boolean;
    proxy: boolean;
    site_proxy: string;
    use_system_proxy: boolean;
    external_checkin_url: string;
    is_pinned: boolean;
    sort_order: number;
    global_weight: number;
    outbound_format_mode: SiteOutboundFormatModeValue;
    custom_header: CustomHeader[];
};

type SiteAccountFormState = {
    site_id: number;
    name: string;
    credential_type: SiteCredentialType;
    username: string;
    password: string;
    access_token: string;
    api_key: string;
    platform_user_id: string;
    account_proxy: string;
    enabled: boolean;
    auto_sync: boolean;
    auto_checkin: boolean;
    random_checkin: boolean;
    checkin_interval_hours: number;
    checkin_random_window_minutes: number;
};

const AUTO_DETECT_VALUE = '__auto__';
const PLATFORM_DEFAULT_VALUE = '__platform_default__';

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

const CREDENTIAL_LABELS: Record<SiteCredentialType, string> = {
    [SiteCredentialType.UsernamePassword]: '用户名 / 密码',
    [SiteCredentialType.AccessToken]: 'Access Token',
    [SiteCredentialType.APIKey]: 'API Key',
};

const OUTBOUND_FORMAT_LABELS: Record<SiteOutboundFormatMode, string> = {
    [SiteOutboundFormatMode.Auto]: '自动按模型拆分',
    [SiteOutboundFormatMode.OpenAIOnly]: '强制 OpenAI 格式',
};

function createEmptySiteForm(): SiteFormState {
    return {
        name: '',
        platform: '',
        base_url: '',
        enabled: true,
        proxy: false,
        site_proxy: '',
        use_system_proxy: false,
        external_checkin_url: '',
        is_pinned: false,
        sort_order: 0,
        global_weight: 1,
        outbound_format_mode: '',
        custom_header: [],
    };
}

function createSiteForm(site: SiteRecord): SiteFormState {
    return {
        name: site.name,
        platform: site.platform,
        base_url: site.base_url,
        enabled: site.enabled,
        proxy: site.proxy,
        site_proxy: site.site_proxy ?? '',
        use_system_proxy: site.use_system_proxy,
        external_checkin_url: site.external_checkin_url ?? '',
        is_pinned: site.is_pinned,
        sort_order: site.sort_order,
        global_weight: site.global_weight,
        outbound_format_mode: site.outbound_format_mode,
        custom_header: site.custom_header.map((item) => ({ ...item })),
    };
}

function defaultCredentialType(platform: SitePlatform): SiteCredentialType {
    switch (platform) {
        case SitePlatform.Sub2API:
            return SiteCredentialType.AccessToken;
        case SitePlatform.OpenAI:
        case SitePlatform.Claude:
        case SitePlatform.Gemini:
            return SiteCredentialType.APIKey;
        default:
            return SiteCredentialType.UsernamePassword;
    }
}

function credentialOptions(platform: SitePlatform) {
    switch (platform) {
        case SitePlatform.Sub2API:
            return [SiteCredentialType.AccessToken, SiteCredentialType.APIKey];
        case SitePlatform.OpenAI:
        case SitePlatform.Claude:
        case SitePlatform.Gemini:
            return [SiteCredentialType.APIKey, SiteCredentialType.AccessToken];
        default:
            return [
                SiteCredentialType.UsernamePassword,
                SiteCredentialType.AccessToken,
                SiteCredentialType.APIKey,
            ];
    }
}

function createEmptyAccountForm(site: SiteRecord): SiteAccountFormState {
    return {
        site_id: site.id,
        name: '',
        credential_type: defaultCredentialType(site.platform),
        username: '',
        password: '',
        access_token: '',
        api_key: '',
        platform_user_id: '',
        account_proxy: '',
        enabled: true,
        auto_sync: true,
        auto_checkin: true,
        random_checkin: false,
        checkin_interval_hours: 24,
        checkin_random_window_minutes: 120,
    };
}

function createAccountForm(account: SiteAccount): SiteAccountFormState {
    return {
        site_id: account.site_id,
        name: account.name,
        credential_type: account.credential_type,
        username: account.username,
        password: account.password,
        access_token: account.access_token,
        api_key: account.api_key,
        platform_user_id: account.platform_user_id ? String(account.platform_user_id) : '',
        account_proxy: account.account_proxy ?? '',
        enabled: account.enabled,
        auto_sync: account.auto_sync,
        auto_checkin: account.auto_checkin,
        random_checkin: account.random_checkin,
        checkin_interval_hours: account.checkin_interval_hours,
        checkin_random_window_minutes: account.checkin_random_window_minutes,
    };
}

function formatDateTime(value?: string | null) {
    if (!value) return '从未执行';
    const date = new Date(value);
    if (Number.isNaN(date.getTime()) || date.getFullYear() <= 1) {
        return '从未执行';
    }
    return date.toLocaleString();
}

function statusLabel(status: string) {
    switch (status) {
        case 'success':
            return '成功';
        case 'failed':
            return '失败';
        case 'skipped':
            return '跳过';
        case 'idle':
        default:
            return '未执行';
    }
}

function statusClassName(status: string) {
    switch (status) {
        case 'success':
            return 'border-emerald-500/20 bg-emerald-500/10 text-emerald-700 dark:text-emerald-300';
        case 'failed':
            return 'border-destructive/20 bg-destructive/10 text-destructive';
        case 'skipped':
            return 'border-amber-500/20 bg-amber-500/10 text-amber-700 dark:text-amber-300';
        case 'idle':
        default:
            return 'border-border bg-muted/40 text-muted-foreground';
    }
}

function getErrorMessage(error: unknown) {
    if (error instanceof Error) return error.message;
    if (typeof error === 'object' && error !== null && 'message' in error) {
        const message = (error as { message?: unknown }).message;
        if (typeof message === 'string') return message;
    }
    return '操作失败';
}
function buildTokenGroupSummary(account: SiteAccount) {
    const map = new Map<string, number>();
    account.tokens.forEach((token) => {
        const key = token.group_name || token.group_key || 'default';
        map.set(key, (map.get(key) ?? 0) + 1);
    });
    return Array.from(map.entries()).map(([group, count]) => `${group} x${count}`);
}

function buildBindingSummary(account: SiteAccount) {
    return account.channel_bindings.map((binding) => `${formatBindingGroupKey(binding.group_key)} -> #${binding.channel_id}`);
}

function formatBindingGroupKey(groupKey: string) {
    const [baseKey, suffix] = (groupKey || 'default').split('::', 2);
    const normalizedBaseKey = baseKey || 'default';
    switch (suffix) {
        case 'anthropic':
            return `${normalizedBaseKey} [Anthropic]`;
        case 'gemini':
            return `${normalizedBaseKey} [Gemini]`;
        default:
            return normalizedBaseKey;
    }
}

function formatOutboundFormatModeBadge(mode: SiteOutboundFormatModeValue) {
    switch (mode) {
        case SiteOutboundFormatMode.Auto:
            return '端点格式: 自动拆分';
        case SiteOutboundFormatMode.OpenAIOnly:
            return '端点格式: OpenAI';
        default:
            return '端点格式: 平台默认';
    }
}

function trimHeaders(items: CustomHeader[]) {
    return items
        .map((item) => ({
            header_key: item.header_key.trim(),
            header_value: item.header_value.trim(),
        }))
        .filter((item) => item.header_key || item.header_value);
}

function SiteMetric({ label, value }: { label: string; value: number | string }) {
    return (
        <div className="rounded-2xl border border-border/60 bg-muted/20 px-4 py-3">
            <div className="text-xs text-muted-foreground">{label}</div>
            <div className="mt-1 text-lg font-semibold">{value}</div>
        </div>
    );
}

function SummaryBlock({
    icon,
    title,
    items,
    empty,
}: {
    icon: ReactNode;
    title: string;
    items: string[];
    empty: string;
}) {
    const visible = items.slice(0, 6);
    const hiddenCount = items.length - visible.length;

    return (
        <div className="rounded-2xl border border-border/60 bg-background/70 p-3">
            <div className="flex items-center gap-2 text-xs font-medium text-muted-foreground">
                {icon}
                <span>{title}</span>
            </div>
            <div className="mt-3 flex flex-wrap gap-2">
                {visible.length > 0 ? (
                    visible.map((item) => (
                        <Badge key={item} variant="outline" className="max-w-full truncate">
                            {item}
                        </Badge>
                    ))
                ) : (
                    <span className="text-xs text-muted-foreground">{empty}</span>
                )}
                {hiddenCount > 0 ? (
                    <Badge variant="outline" className="text-muted-foreground">
                        +{hiddenCount}
                    </Badge>
                ) : null}
            </div>
        </div>
    );
}

export function Site() {
    const { data: sites, isLoading, error } = useSiteList();
    const createSite = useCreateSite();
    const updateSite = useUpdateSite();
    const enableSite = useEnableSite();
    const deleteSite = useDeleteSite();
    const createSiteAccount = useCreateSiteAccount();
    const updateSiteAccount = useUpdateSiteAccount();
    const enableSiteAccount = useEnableSiteAccount();
    const deleteSiteAccount = useDeleteSiteAccount();
    const syncSiteAccount = useSyncSiteAccount();
    const checkinSiteAccount = useCheckinSiteAccount();
    const syncAllSites = useSyncAllSites();
    const checkinAllSites = useCheckinAllSites();
    const importAllAPIHub = useImportAllAPIHub();
    const detectPlatform = useDetectSitePlatform();
    const batchAction = useSiteBatchAction();
    const updateDisabledModels = useUpdateSiteDisabledModels();

    const [siteDialogOpen, setSiteDialogOpen] = useState(false);
    const [importDialogOpen, setImportDialogOpen] = useState(false);
    const [importPayloadText, setImportPayloadText] = useState('');
    const [importFile, setImportFile] = useState<File | null>(null);
    const [lastImportResult, setLastImportResult] = useState<AllAPIHubImportResult | null>(null);
    const [editingSite, setEditingSite] = useState<SiteRecord | null>(null);
    const [siteForm, setSiteForm] = useState<SiteFormState>(createEmptySiteForm());

    const [accountDialogOpen, setAccountDialogOpen] = useState(false);
    const [accountSite, setAccountSite] = useState<SiteRecord | null>(null);
    const [editingAccount, setEditingAccount] = useState<SiteAccount | null>(null);
    const [accountForm, setAccountForm] = useState<SiteAccountFormState | null>(null);

    // Batch selection
    const [selectedSiteIds, setSelectedSiteIds] = useState<number[]>([]);

    // Delete confirmation
    const [deleteConfirm, setDeleteConfirm] = useState<{ type: 'site' | 'account'; id: number; name: string } | null>(null);

    // Disabled models dialog
    const [disabledModelsSiteId, setDisabledModelsSiteId] = useState<number | null>(null);
    const disabledModelsQuery = useSiteDisabledModels(disabledModelsSiteId);
    const availableModelsQuery = useSiteAvailableModels(disabledModelsSiteId);
    const [disabledModelSearch, setDisabledModelSearch] = useState('');

    let siteCount = 0;
    let accountCount = 0;
    let tokenCount = 0;
    let modelCount = 0;

    sites?.forEach((site) => {
        siteCount += 1;
        accountCount += site.accounts.length;
        site.accounts.forEach((account) => {
            tokenCount += account.tokens.length;
            modelCount += account.models.length;
        });
    });

    const currentPlatform = accountSite?.platform ?? SitePlatform.NewAPI;
    const currentCredentialOptions = credentialOptions(currentPlatform);

    function openCreateSiteDialog() {
        setEditingSite(null);
        setSiteForm(createEmptySiteForm());
        setSiteDialogOpen(true);
    }

    function openEditSiteDialog(site: SiteRecord) {
        setEditingSite(site);
        setSiteForm(createSiteForm(site));
        setSiteDialogOpen(true);
    }

    function closeSiteDialog(open: boolean) {
        setSiteDialogOpen(open);
        if (!open) {
            setEditingSite(null);
            setSiteForm(createEmptySiteForm());
        }
    }

    function openCreateAccountDialog(site: SiteRecord) {
        setAccountSite(site);
        setEditingAccount(null);
        setAccountForm(createEmptyAccountForm(site));
        setAccountDialogOpen(true);
    }

    function openEditAccountDialog(site: SiteRecord, account: SiteAccount) {
        setAccountSite(site);
        setEditingAccount(account);
        setAccountForm(createAccountForm(account));
        setAccountDialogOpen(true);
    }

    function closeAccountDialog(open: boolean) {
        setAccountDialogOpen(open);
        if (!open) {
            setAccountSite(null);
            setEditingAccount(null);
            setAccountForm(null);
        }
    }

    async function submitSiteForm(event: FormEvent<HTMLFormElement>) {
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
        if (!platform && !editingSite) {
            try {
                const detected = await detectPlatform.mutateAsync(siteForm.base_url.trim());
                platform = detected.platform as SitePlatform;
                toast.success(`自动检测到平台：${PLATFORM_LABELS[platform] ?? platform}`);
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
        const invalidHeader = customHeader.find((item) => !item.header_key || !item.header_value);
        if (invalidHeader) {
            toast.error('自定义 Header 的键和值都不能为空');
            return;
        }


        const payload = {
            name: siteForm.name.trim(),
            platform: platform as SitePlatform,
            base_url: siteForm.base_url.trim(),
            enabled: siteForm.enabled,
            proxy: siteForm.proxy,
            site_proxy: siteForm.site_proxy.trim(),
            use_system_proxy: siteForm.use_system_proxy,
            external_checkin_url: siteForm.external_checkin_url.trim() || null,
            is_pinned: siteForm.is_pinned,
            sort_order: siteForm.sort_order,
            global_weight: siteForm.global_weight,
            outbound_format_mode: siteForm.outbound_format_mode,
            custom_header: customHeader,
        };

        try {
            if (editingSite) {
                await updateSite.mutateAsync({ id: editingSite.id, ...payload });
                toast.success('站点已更新');
            } else {
                await createSite.mutateAsync(payload);
                toast.success('站点已创建');
            }
            closeSiteDialog(false);
        } catch (submitError) {
            toast.error(getErrorMessage(submitError));
        }
    }
    async function submitAccountForm(event: FormEvent<HTMLFormElement>) {
        event.preventDefault();

        if (!accountSite || !accountForm) {
            toast.error('站点上下文不存在');
            return;
        }
        if (!accountForm.name.trim()) {
            toast.error('请输入账号名称');
            return;
        }

        if (accountForm.credential_type === SiteCredentialType.UsernamePassword) {
            if (!accountForm.username.trim() || !accountForm.password.trim()) {
                toast.error('用户名和密码不能为空');
                return;
            }
        }
        if (accountForm.credential_type === SiteCredentialType.AccessToken && !accountForm.access_token.trim()) {
            toast.error('请输入 Access Token');
            return;
        }
        if (accountForm.credential_type === SiteCredentialType.APIKey && !accountForm.api_key.trim()) {
            toast.error('请输入 API Key');
            return;
        }
        if (accountForm.auto_checkin && accountForm.random_checkin) {
            if (!Number.isFinite(accountForm.checkin_interval_hours) || accountForm.checkin_interval_hours < 1 || accountForm.checkin_interval_hours > 720) {
                toast.error('最小签到间隔必须在 1 到 720 小时之间');
                return;
            }
            if (!Number.isFinite(accountForm.checkin_random_window_minutes) || accountForm.checkin_random_window_minutes < 0 || accountForm.checkin_random_window_minutes > 1440) {
                toast.error('随机延迟窗口必须在 0 到 1440 分钟之间');
                return;
            }
        }

        const parsedPlatformUserID = accountForm.platform_user_id.trim() ? Number(accountForm.platform_user_id.trim()) : null;
        if (parsedPlatformUserID !== null && (!Number.isInteger(parsedPlatformUserID) || parsedPlatformUserID <= 0)) {
            toast.error('Platform User ID 必须是大于 0 的整数');
            return;
        }

        const payload = {
            site_id: accountForm.site_id,
            name: accountForm.name.trim(),
            credential_type: accountForm.credential_type,
            username: accountForm.username.trim(),
            password: accountForm.password.trim(),
            access_token: accountForm.access_token.trim(),
            api_key: accountForm.api_key.trim(),
            platform_user_id: parsedPlatformUserID,
            account_proxy: accountForm.account_proxy.trim(),
            enabled: accountForm.enabled,
            auto_sync: accountForm.auto_sync,
            auto_checkin: accountForm.auto_checkin,
            random_checkin: accountForm.random_checkin,
            checkin_interval_hours: Math.max(1, Math.trunc(accountForm.checkin_interval_hours || 24)),
            checkin_random_window_minutes: Math.max(0, Math.trunc(accountForm.checkin_random_window_minutes || 0)),
        };

        try {
            if (editingAccount) {
                await updateSiteAccount.mutateAsync({ id: editingAccount.id, ...payload });
                toast.success('站点账号已更新');
            } else {
                await createSiteAccount.mutateAsync(payload);
                toast.success('站点账号已创建');
            }
            closeAccountDialog(false);
        } catch (submitError) {
            toast.error(getErrorMessage(submitError));
        }
    }

    async function handleToggleSite(site: SiteRecord) {
        try {
            await enableSite.mutateAsync({ id: site.id, enabled: !site.enabled });
            toast.success(site.enabled ? '站点已停用' : '站点已启用');
        } catch (toggleError) {
            toast.error(getErrorMessage(toggleError));
        }
    }

    async function handleDeleteSite(site: SiteRecord) {
        setDeleteConfirm({ type: 'site', id: site.id, name: site.name });
    }

    async function handleToggleAccount(account: SiteAccount) {
        try {
            await enableSiteAccount.mutateAsync({ id: account.id, enabled: !account.enabled });
            toast.success(account.enabled ? '站点账号已停用' : '站点账号已启用');
        } catch (toggleError) {
            toast.error(getErrorMessage(toggleError));
        }
    }

    async function handleDeleteAccount(account: SiteAccount) {
        setDeleteConfirm({ type: 'account', id: account.id, name: account.name });
    }

    async function handleSyncAccount(account: SiteAccount) {
        try {
            const result = await syncSiteAccount.mutateAsync(account.id);
            toast.success(`同步完成：${result.group_count} 个分组，${result.token_count} 个 key，${result.model_count} 个模型`);
        } catch (syncError) {
            toast.error(getErrorMessage(syncError));
        }
    }

    async function handleCheckinAccount(account: SiteAccount) {
        try {
            const result = await checkinSiteAccount.mutateAsync(account.id);
            const suffix = result.reward ? `，奖励：${result.reward}` : '';
            toast.success(`${statusLabel(result.status)}：${result.message}${suffix}`);
        } catch (checkinError) {
            toast.error(getErrorMessage(checkinError));
        }
    }

    async function handleSyncAll() {
        try {
            await syncAllSites.mutateAsync();
            toast.success('已触发后台全量同步，页面会自动刷新');
        } catch (syncError) {
            toast.error(getErrorMessage(syncError));
        }
    }

    async function handleCheckinAll() {
        try {
            await checkinAllSites.mutateAsync();
            toast.success('已触发后台全量签到，页面会自动刷新');
        } catch (checkinError) {
            toast.error(getErrorMessage(checkinError));
        }
    }

    async function handleImportAllAPIHub() {
        const hasFile = !!importFile;
        const hasText = !!importPayloadText.trim();
        if (!hasFile && !hasText) {
            toast.error('请选择 JSON 文件或粘贴 All API Hub 导出内容');
            return;
        }

        try {
            const result = await importAllAPIHub.mutateAsync({
                file: importFile,
                text: importPayloadText,
            });
            setLastImportResult(result);
            setImportFile(null);
            setImportPayloadText('');
            toast.success(`导入完成：新增 ${result.created_sites} 个站点，新增 ${result.created_accounts} 个账号，更新 ${result.updated_accounts} 个账号`);
        } catch (importError) {
            toast.error(getErrorMessage(importError));
        }
    }

    async function confirmDelete() {
        if (!deleteConfirm) return;
        try {
            if (deleteConfirm.type === 'site') {
                await deleteSite.mutateAsync(deleteConfirm.id);
                toast.success('站点已删除');
                setSelectedSiteIds((prev) => prev.filter((id) => id !== deleteConfirm.id));
            } else {
                await deleteSiteAccount.mutateAsync(deleteConfirm.id);
                toast.success('站点账号已删除');
            }
        } catch (deleteError) {
            toast.error(getErrorMessage(deleteError));
        }
        setDeleteConfirm(null);
    }

    function toggleSiteSelection(siteId: number) {
        setSelectedSiteIds((prev) =>
            prev.includes(siteId) ? prev.filter((id) => id !== siteId) : [...prev, siteId],
        );
    }

    async function handleBatchAction(action: string) {
        if (selectedSiteIds.length === 0) {
            toast.error('请先选择站点');
            return;
        }
        try {
            const result = await batchAction.mutateAsync({ ids: selectedSiteIds, action });
            const successCount = result.success_ids.length;
            const failedCount = result.failed_items.length;
            toast.success(`操作完成：成功 ${successCount}，失败 ${failedCount}`);
            if (action === 'delete') {
                setSelectedSiteIds([]);
            }
        } catch (batchError) {
            toast.error(getErrorMessage(batchError));
        }
    }

    async function handleTogglePin(site: SiteRecord) {
        try {
            await updateSite.mutateAsync({ id: site.id, is_pinned: !site.is_pinned });
            toast.success(site.is_pinned ? '已取消置顶' : '已置顶');
        } catch (pinError) {
            toast.error(getErrorMessage(pinError));
        }
    }

    function formatBalance(value: number) {
        if (value === 0) return '0';
        if (value >= 1000000) return `${(value / 1000000).toFixed(2)}M`;
        if (value >= 1000) return `${(value / 1000).toFixed(2)}K`;
        return value.toFixed(2);
    }

    return (
        <div className="h-full min-h-0 overflow-y-auto overscroll-contain rounded-t-3xl">
            <PageWrapper className="space-y-4 pb-24 md:pb-4">
                <section className="rounded-3xl border border-border bg-card p-6">
                    <div className="flex flex-col gap-4 lg:flex-row lg:items-start lg:justify-between">
                        <div className="max-w-3xl space-y-3">
                            <div className="flex items-center gap-2 text-lg font-semibold">
                                <Globe2 className="size-5" />
                                <span>站点管理</span>
                            </div>
                            <p className="text-sm text-muted-foreground">
                                维护不同平台的站点与账号，自动同步分组、模型和 key，并将每个分组投影为一个或多个托管 channel。同分组下的多个 key 会聚合到同一个 channel。
                            </p>
                            <div className="rounded-2xl border border-border/60 bg-muted/20 px-4 py-3 text-sm text-muted-foreground">
                                当前方案以 <code>site_user_group</code> 作为主拆分维度；无分组时使用 <code>default</code>。当站点启用自动端点格式拆分时，同一分组下会按模型类型继续拆分 Claude、Gemini 和 OpenAI 托管 channel。
                            </div>
                        </div>
                        <div className="flex flex-wrap gap-2">
                            <Button onClick={openCreateSiteDialog} className="rounded-xl">
                                <Plus className="size-4" />新增站点
                            </Button>
                            <Button
                                variant="outline"
                                className="rounded-xl"
                                onClick={() => setImportDialogOpen(true)}
                            >
                                <Upload className="size-4" />导入 All API Hub
                            </Button>
                            <Button variant="outline" onClick={handleSyncAll} disabled={syncAllSites.isPending} className="rounded-xl">
                                <RefreshCw className={cn('size-4', syncAllSites.isPending ? 'animate-spin' : '')} />
                                {syncAllSites.isPending ? '同步中...' : '全量同步'}
                            </Button>
                            <Button variant="outline" onClick={handleCheckinAll} disabled={checkinAllSites.isPending} className="rounded-xl">
                                <CalendarCheck2 className="size-4" />
                                {checkinAllSites.isPending ? '签到中...' : '全量签到'}
                            </Button>
                        </div>
                    </div>

                    <div className="mt-5 grid gap-3 sm:grid-cols-2 xl:grid-cols-4">
                        <SiteMetric label="站点数" value={siteCount} />
                        <SiteMetric label="账号数" value={accountCount} />
                        <SiteMetric label="同步到的 Key" value={tokenCount} />
                        <SiteMetric label="同步到的模型" value={modelCount} />
                    </div>
                </section>

                <CheckinPanel sites={sites} isLoading={isLoading} />

                {selectedSiteIds.length > 0 ? (
                    <section className="rounded-3xl border border-primary/30 bg-primary/5 p-4">
                        <div className="flex flex-wrap items-center gap-3">
                            <span className="text-sm font-medium">已选 {selectedSiteIds.length} 个站点</span>
                            <Button variant="outline" size="sm" className="rounded-xl" onClick={() => handleBatchAction('enable')} disabled={batchAction.isPending}>批量启用</Button>
                            <Button variant="outline" size="sm" className="rounded-xl" onClick={() => handleBatchAction('disable')} disabled={batchAction.isPending}>批量禁用</Button>
                            <Button variant="outline" size="sm" className="rounded-xl" onClick={() => handleBatchAction('enable_system_proxy')} disabled={batchAction.isPending}>批量启用系统代理</Button>
                            <Button variant="outline" size="sm" className="rounded-xl" onClick={() => handleBatchAction('disable_system_proxy')} disabled={batchAction.isPending}>批量禁用系统代理</Button>
                            <Button variant="destructive" size="sm" className="rounded-xl" onClick={() => handleBatchAction('delete')} disabled={batchAction.isPending}>批量删除</Button>
                            <Button variant="ghost" size="sm" className="rounded-xl" onClick={() => setSelectedSiteIds([])}>取消选择</Button>
                        </div>
                    </section>
                ) : null}

                {error ? (
                    <section className="rounded-3xl border border-destructive/30 bg-destructive/5 p-6 text-sm text-destructive">
                        站点列表加载失败：{getErrorMessage(error)}
                    </section>
                ) : null}

                {isLoading ? (
                    <section className="rounded-3xl border border-border bg-card p-6 text-sm text-muted-foreground">
                        正在加载站点信息...
                    </section>
                ) : null}

                {!isLoading && !error && (!sites || sites.length === 0) ? (
                    <section className="rounded-3xl border border-dashed border-border bg-card p-10 text-center">
                        <CircleAlert className="mx-auto size-8 text-muted-foreground" />
                        <div className="mt-4 text-lg font-semibold">还没有站点</div>
                        <p className="mt-2 text-sm text-muted-foreground">先新增一个站点，再为它配置账号，后续即可自动同步分组、模型和托管渠道。</p>
                        <Button onClick={openCreateSiteDialog} className="mt-5 rounded-xl">
                            <Plus className="size-4" />新增第一个站点
                        </Button>
                    </section>
                ) : null}

                {sites?.map((site) => (
                    <section key={site.id} className="rounded-3xl border border-border bg-card p-6">
                        <div className="flex flex-col gap-4 xl:flex-row xl:items-start xl:justify-between">
                            <div className="space-y-3">
                                <div className="flex flex-wrap items-center gap-2">
                                    <button type="button" className="shrink-0" onClick={() => toggleSiteSelection(site.id)}>
                                        {selectedSiteIds.includes(site.id) ? <CheckSquare className="size-5 text-primary" /> : <Square className="size-5 text-muted-foreground" />}
                                    </button>
                                    <h2 className="text-xl font-semibold">{site.name}</h2>
                                    {site.is_pinned ? <Badge variant="outline" className="text-amber-600"><Pin className="mr-1 size-3" />置顶</Badge> : null}
                                    <Badge variant="outline">{PLATFORM_LABELS[site.platform]}</Badge>
                                    <Badge variant="outline" className={site.enabled ? 'text-emerald-600' : 'text-muted-foreground'}>
                                        {site.enabled ? '启用中' : '已停用'}
                                    </Badge>
                                    <Badge variant="outline">{site.accounts.length} 个账号</Badge>
                                    {site.accounts.length > 0 ? (
                                        <Badge variant="outline" className="gap-1">
                                            <Wallet className="size-3" />
                                            {formatBalance(site.accounts.reduce((sum, acc) => sum + acc.balance, 0))}
                                        </Badge>
                                    ) : null}
                                </div>
                                <div className="flex flex-wrap items-center gap-2 text-sm text-muted-foreground">
                                    <Link2 className="size-4" />
                                    <span>{site.base_url}</span>
                                </div>
                                <div className="flex flex-wrap gap-2 text-xs">
                                    <Badge variant="outline">{site.proxy ? '启用代理' : site.use_system_proxy ? '系统代理' : '直连'}</Badge>
                                    {site.site_proxy ? <Badge variant="outline">{site.site_proxy}</Badge> : null}
                                    <Badge variant="outline">{formatOutboundFormatModeBadge(site.outbound_format_mode)}</Badge>
                                    <Badge variant="outline">{site.custom_header.length} 个自定义 Header</Badge>
                                    {site.external_checkin_url ? <Badge variant="outline">外部签到</Badge> : null}
                                </div>
                            </div>
                            <div className="flex flex-wrap gap-2">
                                <Button variant="outline" size="sm" className="rounded-xl" onClick={() => openCreateAccountDialog(site)}>
                                    <Plus className="size-4" />新增账号
                                </Button>
                                <Button variant="outline" size="sm" className="rounded-xl" onClick={() => handleTogglePin(site)}>
                                    {site.is_pinned ? <PinOff className="size-4" /> : <Pin className="size-4" />}
                                    {site.is_pinned ? '取消置顶' : '置顶'}
                                </Button>
                                <Button variant="outline" size="sm" className="rounded-xl" onClick={() => { setDisabledModelsSiteId(site.id); setDisabledModelSearch(''); }}>
                                    <ShieldOff className="size-4" />禁用模型
                                </Button>
                                <Button variant="outline" size="sm" className="rounded-xl" onClick={() => openEditSiteDialog(site)}>
                                    <Pencil className="size-4" />编辑
                                </Button>
                                <Button variant="outline" size="sm" className="rounded-xl" onClick={() => handleToggleSite(site)}>
                                    {site.enabled ? '停用' : '启用'}
                                </Button>
                                <Button variant="destructive" size="sm" className="rounded-xl" onClick={() => handleDeleteSite(site)}>
                                    <Trash2 className="size-4" />删除
                                </Button>
                            </div>
                        </div>

                        <div className="mt-6 space-y-3">
                            {site.accounts.length === 0 ? (
                                <div className="rounded-2xl border border-dashed border-border/70 bg-muted/10 px-4 py-6 text-sm text-muted-foreground">
                                    暂无账号。添加账号后即可自动同步分组、模型和渠道绑定。
                                </div>
                            ) : null}
                            {site.accounts.map((account) => (
                                <article key={account.id} className="rounded-2xl border border-border/70 bg-muted/10 p-4">
                                    <div className="flex flex-col gap-4 xl:flex-row xl:items-start xl:justify-between">
                                        <div className="min-w-0 flex-1 space-y-4">
                                            <div className="flex flex-wrap items-center gap-2">
                                                <div className="text-base font-semibold">{account.name}</div>
                                                <Badge variant="outline">{CREDENTIAL_LABELS[account.credential_type]}</Badge>
                                                <Badge variant="outline" className={account.enabled ? 'text-emerald-600' : 'text-muted-foreground'}>
                                                    {account.enabled ? '启用中' : '已停用'}
                                                </Badge>
                                                {account.auto_sync ? <Badge variant="outline">自动同步</Badge> : null}
                                                {account.auto_checkin ? <Badge variant="outline">自动签到</Badge> : null}
                                                {account.random_checkin ? <Badge variant="outline">随机时间</Badge> : null}
                                                {account.account_proxy ? <Badge variant="outline">{account.account_proxy}</Badge> : null}
                                            </div>

                                            <div className="grid gap-3 sm:grid-cols-2 xl:grid-cols-5">
                                                <SiteMetric label="分组" value={account.user_groups.length} />
                                                <SiteMetric label="Key" value={account.tokens.length} />
                                                <SiteMetric label="模型" value={account.models.length} />
                                                <SiteMetric label="托管渠道" value={account.channel_bindings.length} />
                                                <SiteMetric label="余额" value={formatBalance(account.balance)} />
                                            </div>

                                            <div className="grid gap-3 lg:grid-cols-2">
                                                <div className="rounded-2xl border border-border/60 bg-background/70 p-4">
                                                    <div className="flex items-center justify-between gap-2">
                                                        <span className="text-sm font-medium">同步状态</span>
                                                        <Badge variant="outline" className={statusClassName(account.last_sync_status)}>
                                                            {statusLabel(account.last_sync_status)}
                                                        </Badge>
                                                    </div>
                                                    <div className="mt-2 text-xs text-muted-foreground">上次同步：{formatDateTime(account.last_sync_at)}</div>
                                                    <div className="mt-2 text-sm break-all">{account.last_sync_message || '等待首次同步'}</div>
                                                </div>

                                                <div className="rounded-2xl border border-border/60 bg-background/70 p-4">
                                                    <div className="flex items-center justify-between gap-2">
                                                        <span className="text-sm font-medium">签到状态</span>
                                                        <Badge variant="outline" className={statusClassName(account.last_checkin_status)}>
                                                            {statusLabel(account.last_checkin_status)}
                                                        </Badge>
                                                    </div>
                                                    <div className="mt-2 text-xs text-muted-foreground">上次签到：{formatDateTime(account.last_checkin_at)}</div>
                                                    {account.auto_checkin && account.random_checkin ? (
                                                        <div className="mt-1 text-xs text-muted-foreground">
                                                            下次自动签到：{account.next_auto_checkin_at ? formatDateTime(account.next_auto_checkin_at) : '待调度'} · 最小间隔 {account.checkin_interval_hours} 小时 · 随机延迟 0-{account.checkin_random_window_minutes} 分钟
                                                        </div>
                                                    ) : null}
                                                    <div className="mt-2 text-sm break-all">{account.last_checkin_message || '等待首次签到'}</div>
                                                </div>
                                            </div>

                                            <div className="grid gap-3 xl:grid-cols-4">
                                                <SummaryBlock icon={<FolderTree className="size-4" />} title="分组" items={account.user_groups.map((item) => item.name || item.group_key)} empty="暂无分组" />
                                                <SummaryBlock icon={<KeyRound className="size-4" />} title="Key 分组" items={buildTokenGroupSummary(account)} empty="暂无可用 key" />
                                                <SummaryBlock icon={<Sparkles className="size-4" />} title="模型" items={account.models.map((item) => item.model_name)} empty="暂无模型" />
                                                <SummaryBlock icon={<Cable className="size-4" />} title="托管渠道" items={buildBindingSummary(account)} empty="暂无绑定" />
                                            </div>
                                        </div>

                                        <div className="flex flex-wrap gap-2 xl:w-36 xl:flex-col">
                                            <Button variant="outline" size="sm" className="rounded-xl" onClick={() => handleSyncAccount(account)} disabled={syncSiteAccount.isPending}>
                                                <RefreshCw className={cn('size-4', syncSiteAccount.isPending ? 'animate-spin' : '')} />同步
                                            </Button>
                                            <Button variant="outline" size="sm" className="rounded-xl" onClick={() => handleCheckinAccount(account)} disabled={checkinSiteAccount.isPending}>
                                                <CalendarCheck2 className="size-4" />签到
                                            </Button>
                                            <Button variant="outline" size="sm" className="rounded-xl" onClick={() => openEditAccountDialog(site, account)}>
                                                <Pencil className="size-4" />编辑
                                            </Button>
                                            <Button variant="outline" size="sm" className="rounded-xl" onClick={() => handleToggleAccount(account)}>
                                                {account.enabled ? '停用' : '启用'}
                                            </Button>
                                            <Button variant="destructive" size="sm" className="rounded-xl" onClick={() => handleDeleteAccount(account)}>
                                                <Trash2 className="size-4" />删除
                                            </Button>
                                        </div>
                                    </div>
                                </article>
                            ))}
                        </div>
                    </section>
                ))}
            </PageWrapper>

            <Dialog open={siteDialogOpen} onOpenChange={closeSiteDialog}>
                <DialogContent className="max-w-4xl rounded-3xl">
                    <DialogHeader>
                        <DialogTitle>{editingSite ? '编辑站点' : '新增站点'}</DialogTitle>
                        <DialogDescription>配置站点平台、代理和自定义 Header。站点账号会基于这里的基础信息进行同步。</DialogDescription>
                    </DialogHeader>

                    <form className="space-y-5" onSubmit={submitSiteForm}>
                        <div className="grid gap-4 md:grid-cols-2">
                            <label className="grid gap-2 text-sm">
                                <span className="font-medium">站点名称</span>
                                <Input value={siteForm.name} onChange={(event) => setSiteForm((current) => ({ ...current, name: event.target.value }))} placeholder="例如：主站 OneAPI" className="rounded-xl" />
                            </label>

                            <label className="grid gap-2 text-sm">
                                <span className="font-medium">平台类型</span>
                                <Select value={siteForm.platform || AUTO_DETECT_VALUE} onValueChange={(value) => setSiteForm((current) => ({ ...current, platform: value === AUTO_DETECT_VALUE ? '' : value as SitePlatform }))}>
                                    <SelectTrigger className="w-full rounded-xl">
                                        <SelectValue placeholder="自动检测" />
                                    </SelectTrigger>
                                    <SelectContent>
                                        <SelectItem value={AUTO_DETECT_VALUE}>自动检测</SelectItem>
                                        {Object.entries(PLATFORM_LABELS).map(([value, label]) => (
                                            <SelectItem key={value} value={value}>{label}</SelectItem>
                                        ))}
                                    </SelectContent>
                                </Select>
                                <span className="text-xs text-muted-foreground">留空可根据站点地址自动检测平台类型</span>
                            </label>
                        </div>

                        <label className="grid gap-2 text-sm">
                            <span className="font-medium">站点地址</span>
                            <Input value={siteForm.base_url} onChange={(event) => setSiteForm((current) => ({ ...current, base_url: event.target.value }))} placeholder="https://example.com" className="rounded-xl" />
                        </label>

                        <label className="grid gap-2 text-sm">
                            <span className="font-medium">投影端点格式</span>
                            <Select
                                value={siteForm.outbound_format_mode || PLATFORM_DEFAULT_VALUE}
                                onValueChange={(value) => setSiteForm((current) => ({
                                    ...current,
                                    outbound_format_mode: value === PLATFORM_DEFAULT_VALUE ? '' : value as SiteOutboundFormatMode,
                                }))}
                            >
                                <SelectTrigger className="w-full rounded-xl">
                                    <SelectValue placeholder="平台默认" />
                                </SelectTrigger>
                                <SelectContent>
                                    <SelectItem value={PLATFORM_DEFAULT_VALUE}>平台默认</SelectItem>
                                    {Object.entries(OUTBOUND_FORMAT_LABELS).map(([value, label]) => (
                                        <SelectItem key={value} value={value}>{label}</SelectItem>
                                    ))}
                                </SelectContent>
                            </Select>
                            <span className="text-xs text-muted-foreground">
                                平台默认下，Claude 和 Gemini 站点保持原生格式，多模型兼容站点会按模型名称自动拆分；如果上游只支持 <code>/chat/completions</code>，可改为强制 OpenAI 格式。
                            </span>
                        </label>

                        <div className="grid gap-4 md:grid-cols-3">
                            <div className="flex items-center justify-between rounded-2xl border border-border/60 bg-muted/20 px-4 py-3">
                                <div>
                                    <div className="text-sm font-medium">启用站点</div>
                                    <div className="text-xs text-muted-foreground">停用后不再投影托管渠道</div>
                                </div>
                                <Switch checked={siteForm.enabled} onCheckedChange={(checked) => setSiteForm((current) => ({ ...current, enabled: checked }))} />
                            </div>

                            <div className="flex items-center justify-between rounded-2xl border border-border/60 bg-muted/20 px-4 py-3">
                                <div>
                                    <div className="text-sm font-medium">站点代理</div>
                                    <div className="text-xs text-muted-foreground">请求时走站点级代理</div>
                                </div>
                                <Switch checked={siteForm.proxy} onCheckedChange={(checked) => setSiteForm((current) => ({ ...current, proxy: checked }))} />
                            </div>

                            <div className="flex items-center justify-between rounded-2xl border border-border/60 bg-muted/20 px-4 py-3">
                                <div>
                                    <div className="text-sm font-medium">系统代理</div>
                                    <div className="text-xs text-muted-foreground">无专用代理时用全局代理</div>
                                </div>
                                <Switch checked={siteForm.use_system_proxy} onCheckedChange={(checked) => setSiteForm((current) => ({ ...current, use_system_proxy: checked }))} />
                            </div>
                        </div>

                        <label className="grid gap-2 text-sm">
                            <span className="font-medium">站点级代理</span>
                            <Input value={siteForm.site_proxy} onChange={(event) => setSiteForm((current) => ({ ...current, site_proxy: event.target.value }))} placeholder="可选：例如 socks5://127.0.0.1:7890" className="rounded-xl" />
                        </label>

                        <label className="grid gap-2 text-sm">
                            <span className="font-medium">外部签到 URL</span>
                            <Input value={siteForm.external_checkin_url} onChange={(event) => setSiteForm((current) => ({ ...current, external_checkin_url: event.target.value }))} placeholder="可选：例如 https://example.com/api/checkin" className="rounded-xl" />
                            <span className="text-xs text-muted-foreground">配置后签到将调用此 URL，而非内置平台逻辑。适用于平台不支持签到但有外部签到接口的场景。</span>
                        </label>

                        <div className="space-y-3 rounded-2xl border border-border/60 bg-muted/10 p-4">
                            <div className="flex items-center justify-between gap-3">
                                <div>
                                    <div className="text-sm font-medium">自定义 Header</div>
                                    <div className="text-xs text-muted-foreground">会透传到站点接口请求，可用于附加鉴权或租户信息</div>
                                </div>
                                <Button type="button" variant="outline" size="sm" className="rounded-xl" onClick={() => setSiteForm((current) => ({ ...current, custom_header: [...current.custom_header, { header_key: '', header_value: '' }] }))}>
                                    <Plus className="size-4" />添加 Header
                                </Button>
                            </div>

                            {siteForm.custom_header.length === 0 ? <div className="text-sm text-muted-foreground">暂无自定义 Header</div> : null}
                            <div className="space-y-3">
                                {siteForm.custom_header.map((item, index) => (
                                    <div key={`${index}-${item.header_key}`} className="grid gap-3 md:grid-cols-[1fr_1fr_auto]">
                                        <Input
                                            value={item.header_key}
                                            onChange={(event) => setSiteForm((current) => ({
                                                ...current,
                                                custom_header: current.custom_header.map((header, headerIndex) => headerIndex === index ? { ...header, header_key: event.target.value } : header),
                                            }))}
                                            placeholder="Header Key"
                                            className="rounded-xl"
                                        />
                                        <Input
                                            value={item.header_value}
                                            onChange={(event) => setSiteForm((current) => ({
                                                ...current,
                                                custom_header: current.custom_header.map((header, headerIndex) => headerIndex === index ? { ...header, header_value: event.target.value } : header),
                                            }))}
                                            placeholder="Header Value"
                                            className="rounded-xl"
                                        />
                                        <Button type="button" variant="outline" size="icon" className="rounded-xl" onClick={() => setSiteForm((current) => ({ ...current, custom_header: current.custom_header.filter((_, headerIndex) => headerIndex !== index) }))}>
                                            <X className="size-4" />
                                        </Button>
                                    </div>
                                ))}
                            </div>
                        </div>

                        <DialogFooter>
                            <Button type="button" variant="outline" className="rounded-xl" onClick={() => closeSiteDialog(false)}>取消</Button>
                            <Button type="submit" className="rounded-xl" disabled={createSite.isPending || updateSite.isPending}>
                                {createSite.isPending || updateSite.isPending ? '保存中...' : '保存'}
                            </Button>
                        </DialogFooter>
                    </form>
                </DialogContent>
            </Dialog>

            <Dialog open={accountDialogOpen} onOpenChange={closeAccountDialog}>
                <DialogContent className="max-w-3xl rounded-3xl">
                    <DialogHeader>
                        <DialogTitle>{editingAccount ? '编辑站点账号' : '新增站点账号'}</DialogTitle>
                        <DialogDescription>
                            {accountSite ? `账号会挂载到站点「${accountSite.name}」下，并按同步结果自动投影 channel。` : '配置站点账号后，可自动同步分组、模型和托管渠道。'}
                        </DialogDescription>
                    </DialogHeader>

                    {accountForm ? (
                        <form className="space-y-5" onSubmit={submitAccountForm}>
                            <div className="grid gap-4 md:grid-cols-2">
                                <label className="grid gap-2 text-sm">
                                    <span className="font-medium">账号名称</span>
                                    <Input value={accountForm.name} onChange={(event) => setAccountForm((current) => current ? { ...current, name: event.target.value } : current)} placeholder="例如：主账号" className="rounded-xl" />
                                </label>

                                <label className="grid gap-2 text-sm">
                                    <span className="font-medium">凭据类型</span>
                                    <Select value={accountForm.credential_type} onValueChange={(value) => setAccountForm((current) => current ? { ...current, credential_type: value as SiteCredentialType } : current)}>
                                        <SelectTrigger className="w-full rounded-xl">
                                            <SelectValue />
                                        </SelectTrigger>
                                        <SelectContent>
                                            {currentCredentialOptions.map((value) => (
                                                <SelectItem key={value} value={value}>{CREDENTIAL_LABELS[value]}</SelectItem>
                                            ))}
                                        </SelectContent>
                                    </Select>
                                </label>
                            </div>

                            {accountForm.credential_type === SiteCredentialType.UsernamePassword ? (
                                <div className="grid gap-4 md:grid-cols-2">
                                    <label className="grid gap-2 text-sm">
                                        <span className="font-medium">用户名</span>
                                        <Input value={accountForm.username} onChange={(event) => setAccountForm((current) => current ? { ...current, username: event.target.value } : current)} placeholder="请输入用户名" className="rounded-xl" />
                                    </label>

                                    <label className="grid gap-2 text-sm">
                                        <span className="font-medium">密码</span>
                                        <Input type="password" value={accountForm.password} onChange={(event) => setAccountForm((current) => current ? { ...current, password: event.target.value } : current)} placeholder="请输入密码" className="rounded-xl" />
                                    </label>
                                </div>
                            ) : null}

                            {accountForm.credential_type === SiteCredentialType.AccessToken ? (
                                <label className="grid gap-2 text-sm">
                                    <span className="font-medium">Access Token</span>
                                    <Input value={accountForm.access_token} onChange={(event) => setAccountForm((current) => current ? { ...current, access_token: event.target.value } : current)} placeholder="请输入 Access Token" className="rounded-xl" />
                                </label>
                            ) : null}

                            {accountForm.credential_type === SiteCredentialType.APIKey ? (
                                <label className="grid gap-2 text-sm">
                                    <span className="font-medium">API Key</span>
                                    <Input value={accountForm.api_key} onChange={(event) => setAccountForm((current) => current ? { ...current, api_key: event.target.value } : current)} placeholder="请输入 API Key" className="rounded-xl" />
                                </label>
                            ) : null}

                            {currentPlatform === SitePlatform.NewAPI ? (
                                <label className="grid gap-2 text-sm">
                                    <span className="font-medium">Platform User ID</span>
                                    <Input
                                        value={accountForm.platform_user_id}
                                        onChange={(event) => setAccountForm((current) => current ? { ...current, platform_user_id: event.target.value } : current)}
                                        placeholder="可选：例如 11494"
                                        className="rounded-xl"
                                    />
                                    <span className="text-xs text-muted-foreground">部分 New API 站点同步 token、分组和签到时要求额外提供用户 ID。All API Hub 导入会自动填充该值。</span>
                                </label>
                            ) : null}

                            <label className="grid gap-2 text-sm">
                                <span className="font-medium">账号级代理</span>
                                <Input
                                    value={accountForm.account_proxy}
                                    onChange={(event) => setAccountForm((current) => current ? { ...current, account_proxy: event.target.value } : current)}
                                    placeholder="可选：例如 socks5://127.0.0.1:7890"
                                    className="rounded-xl"
                                />
                                <span className="text-xs text-muted-foreground">用于该账号的同步、签到和模型拉取；自动投影的 channel 默认也会跟随这里的代理。</span>
                            </label>

                            <div className="grid gap-4 md:grid-cols-2">
                                <div className="flex items-center justify-between rounded-2xl border border-border/60 bg-muted/20 px-4 py-3">
                                    <div>
                                        <div className="flex items-center gap-2"><UserRound className="size-4 text-muted-foreground" /><span className="text-sm font-medium">启用账号</span></div>
                                        <div className="text-xs text-muted-foreground">停用后不参与同步和签到</div>
                                    </div>
                                    <Switch checked={accountForm.enabled} onCheckedChange={(checked) => setAccountForm((current) => current ? { ...current, enabled: checked } : current)} />
                                </div>

                                <div className="flex items-center justify-between rounded-2xl border border-border/60 bg-muted/20 px-4 py-3">
                                    <div>
                                        <div className="flex items-center gap-2"><RefreshCw className="size-4 text-muted-foreground" /><span className="text-sm font-medium">自动同步</span></div>
                                        <div className="text-xs text-muted-foreground">定时拉取分组、模型和 key</div>
                                    </div>
                                    <Switch checked={accountForm.auto_sync} onCheckedChange={(checked) => setAccountForm((current) => current ? { ...current, auto_sync: checked } : current)} />
                                </div>

                                <div className="flex items-center justify-between rounded-2xl border border-border/60 bg-muted/20 px-4 py-3">
                                    <div>
                                        <div className="flex items-center gap-2"><CalendarCheck2 className="size-4 text-muted-foreground" /><span className="text-sm font-medium">自动签到</span></div>
                                        <div className="text-xs text-muted-foreground">定时执行平台签到</div>
                                    </div>
                                    <Switch checked={accountForm.auto_checkin} onCheckedChange={(checked) => setAccountForm((current) => current ? { ...current, auto_checkin: checked } : current)} />
                                </div>

                                <div className="flex items-center justify-between rounded-2xl border border-border/60 bg-muted/20 px-4 py-3">
                                    <div>
                                        <div className="flex items-center gap-2"><CalendarCheck2 className="size-4 text-muted-foreground" /><span className="text-sm font-medium">随机签到</span></div>
                                        <div className="text-xs text-muted-foreground">在间隔基础上添加随机延迟</div>
                                    </div>
                                    <Switch checked={accountForm.random_checkin} onCheckedChange={(checked) => setAccountForm((current) => current ? { ...current, random_checkin: checked } : current)} />
                                </div>
                            </div>

                            {accountForm.auto_checkin && accountForm.random_checkin ? (
                                <div className="grid gap-4 md:grid-cols-2">
                                    <label className="grid gap-2 text-sm">
                                        <span className="font-medium">最小签到间隔（小时）</span>
                                        <Input
                                            type="number"
                                            min={1}
                                            max={720}
                                            value={accountForm.checkin_interval_hours}
                                            onChange={(event) => setAccountForm((current) => current ? { ...current, checkin_interval_hours: Number(event.target.value) } : current)}
                                            placeholder="24"
                                            className="rounded-xl"
                                        />
                                    </label>

                                    <label className="grid gap-2 text-sm">
                                        <span className="font-medium">随机延迟窗口（分钟）</span>
                                        <Input
                                            type="number"
                                            min={0}
                                            max={1440}
                                            value={accountForm.checkin_random_window_minutes}
                                            onChange={(event) => setAccountForm((current) => current ? { ...current, checkin_random_window_minutes: Number(event.target.value) } : current)}
                                            placeholder="120"
                                            className="rounded-xl"
                                        />
                                    </label>
                                </div>
                            ) : null}

                            <div className="grid gap-3 rounded-2xl border border-border/60 bg-muted/10 p-4 text-sm text-muted-foreground">
                                <div className="flex items-center gap-2"><Waypoints className="size-4" /><span>投影规则说明</span></div>
                                <p>同步后会以 <code>site_user_group</code> 为主维度生成托管 channel；无分组时使用 <code>default</code>。同一分组下的多个 key 会聚合到同一个 channel；如果站点启用了自动端点格式拆分，Claude 和 Gemini 模型会继续拆成独立托管 channel。</p>
                                {accountForm.auto_checkin && accountForm.random_checkin ? (
                                    <p>随机签到会基于“上次成功签到时间 + 最小间隔 + 0 到随机延迟窗口”的规则生成下次执行时间，适合需要接近 24 小时间隔的站点。</p>
                                ) : null}
                            </div>

                            <DialogFooter>
                                <Button type="button" variant="outline" className="rounded-xl" onClick={() => closeAccountDialog(false)}>取消</Button>
                                <Button type="submit" className="rounded-xl" disabled={createSiteAccount.isPending || updateSiteAccount.isPending}>
                                    {createSiteAccount.isPending || updateSiteAccount.isPending ? '保存中...' : '保存'}
                                </Button>
                            </DialogFooter>
                        </form>
                    ) : null}
                </DialogContent>
            </Dialog>

            <Dialog open={importDialogOpen} onOpenChange={(open) => { setImportDialogOpen(open); if (!open) setLastImportResult(null); }}>
                <DialogContent className="max-w-3xl rounded-3xl max-h-[85vh] overflow-y-auto">
                    <DialogHeader>
                        <DialogTitle className="flex items-center gap-2"><FileJson className="size-5" />导入 All API Hub 账号</DialogTitle>
                        <DialogDescription>
                            支持直接上传 All API Hub 导出的 JSON 文件，或粘贴完整导出内容。导入后会按平台和站点地址自动创建或复用站点，并为可同步账号触发后台同步。
                        </DialogDescription>
                    </DialogHeader>

                    <div className="space-y-5">
                        <div className="space-y-3 rounded-2xl border border-border/60 bg-muted/10 p-4">
                            <div className="text-sm font-medium">上传 JSON 文件</div>
                            <Input
                                type="file"
                                accept=".json,application/json"
                                onChange={(event) => {
                                    setImportFile(event.target.files?.[0] ?? null);
                                    setLastImportResult(null);
                                }}
                                className="rounded-xl"
                            />
                            <div className="text-xs text-muted-foreground">
                                {importFile ? `已选择：${importFile.name}` : '支持 All API Hub 导出的 .json 文件'}
                            </div>
                            {importFile ? (
                                <Button type="button" variant="outline" size="sm" className="rounded-xl" onClick={() => setImportFile(null)}>
                                    <X className="size-4" />清除文件
                                </Button>
                            ) : null}
                        </div>

                        <label className="grid gap-2 text-sm">
                            <span className="font-medium">或粘贴导出 JSON</span>
                            <textarea
                                value={importPayloadText}
                                onChange={(event) => {
                                    setImportPayloadText(event.target.value);
                                    setLastImportResult(null);
                                }}
                                placeholder='粘贴类似 {"accounts":{"accounts":[...]}} 的完整导出内容'
                                className="min-h-40 rounded-2xl border border-input bg-background px-4 py-3 font-mono text-xs outline-none transition focus-visible:border-ring focus-visible:ring-[3px] focus-visible:ring-ring/20"
                            />
                            <span className="text-xs text-muted-foreground">
                                导入会保留已存在站点的本地配置；同一分组下的多个 key 后续仍会聚合到同一个托管 channel。
                            </span>
                        </label>

                        {lastImportResult ? (
                            <div className="space-y-4 rounded-2xl border border-border/60 bg-muted/10 p-4">
                                <div className="grid gap-3 sm:grid-cols-3">
                                    <SiteMetric label="新增站点" value={lastImportResult.created_sites} />
                                    <SiteMetric label="复用站点" value={lastImportResult.reused_sites} />
                                    <SiteMetric label="新增账号" value={lastImportResult.created_accounts} />
                                    <SiteMetric label="更新账号" value={lastImportResult.updated_accounts} />
                                    <SiteMetric label="跳过账号" value={lastImportResult.skipped_accounts} />
                                    <SiteMetric label="后台同步" value={lastImportResult.scheduled_sync_accounts} />
                                </div>

                                {lastImportResult.warnings.length > 0 ? (
                                    <div className="rounded-2xl border border-border/60 bg-background/70 p-4">
                                        <div className="flex items-center gap-2 text-sm font-medium">
                                            <TriangleAlert className="size-4 text-muted-foreground" />
                                            <span>导入告警</span>
                                        </div>
                                        <div className="mt-3 space-y-2 text-sm text-muted-foreground">
                                            {lastImportResult.warnings.map((warning) => (
                                                <div key={warning} className="break-all rounded-xl border border-border/60 bg-muted/20 px-3 py-2">
                                                    {warning}
                                                </div>
                                            ))}
                                        </div>
                                    </div>
                                ) : null}
                            </div>
                        ) : null}
                    </div>

                    <DialogFooter>
                        <Button variant="outline" className="rounded-xl" onClick={() => setImportDialogOpen(false)}>关闭</Button>
                        <Button onClick={handleImportAllAPIHub} disabled={importAllAPIHub.isPending} className="rounded-xl">
                            <Upload className={cn('size-4', importAllAPIHub.isPending ? 'animate-pulse' : '')} />
                            {importAllAPIHub.isPending ? '导入中...' : '开始导入'}
                        </Button>
                    </DialogFooter>
                </DialogContent>
            </Dialog>

            <Dialog open={!!deleteConfirm} onOpenChange={(open) => { if (!open) setDeleteConfirm(null); }}>
                <DialogContent className="max-w-md rounded-3xl">
                    <DialogHeader>
                        <DialogTitle>确认删除</DialogTitle>
                        <DialogDescription>
                            {deleteConfirm?.type === 'site'
                                ? `确认删除站点「${deleteConfirm?.name}」及其所有账号和托管渠道？此操作不可撤销。`
                                : `确认删除账号「${deleteConfirm?.name}」及其托管渠道？此操作不可撤销。`}
                        </DialogDescription>
                    </DialogHeader>
                    <DialogFooter>
                        <Button variant="outline" className="rounded-xl" onClick={() => setDeleteConfirm(null)}>取消</Button>
                        <Button variant="destructive" className="rounded-xl" onClick={confirmDelete} disabled={deleteSite.isPending || deleteSiteAccount.isPending}>
                            {deleteSite.isPending || deleteSiteAccount.isPending ? '删除中...' : '确认删除'}
                        </Button>
                    </DialogFooter>
                </DialogContent>
            </Dialog>

            <Dialog open={disabledModelsSiteId != null} onOpenChange={(open) => { if (!open) setDisabledModelsSiteId(null); }}>
                <DialogContent className="max-w-3xl rounded-3xl max-h-[80vh] overflow-y-auto">
                    <DialogHeader>
                        <DialogTitle>管理禁用模型</DialogTitle>
                        <DialogDescription>选中的模型将在投影到渠道时被排除。修改后会自动重新投影。</DialogDescription>
                    </DialogHeader>

                    <div className="relative">
                        <Search className="absolute left-3 top-1/2 size-4 -translate-y-1/2 text-muted-foreground" />
                        <Input
                            value={disabledModelSearch}
                            onChange={(event) => setDisabledModelSearch(event.target.value)}
                            placeholder="搜索模型..."
                            className="rounded-xl pl-10"
                        />
                    </div>

                    {disabledModelsQuery.isLoading || availableModelsQuery.isLoading ? (
                        <div className="py-8 text-center text-sm text-muted-foreground">加载中...</div>
                    ) : (
                        <div className="space-y-2 max-h-96 overflow-y-auto">
                            {(() => {
                                const disabledSet = new Set(disabledModelsQuery.data?.models ?? []);
                                const allModels = Array.from(new Set([...(availableModelsQuery.data?.models ?? []), ...disabledSet])).sort();
                                const filtered = disabledModelSearch.trim()
                                    ? allModels.filter((m) => m.toLowerCase().includes(disabledModelSearch.toLowerCase()))
                                    : allModels;

                                if (filtered.length === 0) {
                                    return <div className="py-8 text-center text-sm text-muted-foreground">暂无可用模型</div>;
                                }

                                return filtered.map((modelName) => {
                                    const isDisabled = disabledSet.has(modelName);
                                    return (
                                        <button
                                            key={modelName}
                                            type="button"
                                            className={cn(
                                                'flex w-full items-center gap-3 rounded-xl border px-3 py-2 text-left text-sm transition',
                                                isDisabled
                                                    ? 'border-destructive/20 bg-destructive/5 text-destructive'
                                                    : 'border-border/60 bg-background hover:bg-muted/30',
                                            )}
                                            onClick={async () => {
                                                if (!disabledModelsSiteId) return;
                                                const current = disabledModelsQuery.data?.models ?? [];
                                                const next = isDisabled
                                                    ? current.filter((m) => m !== modelName)
                                                    : [...current, modelName];
                                                try {
                                                    await updateDisabledModels.mutateAsync({ siteId: disabledModelsSiteId, models: next });
                                                } catch (updateError) {
                                                    toast.error(getErrorMessage(updateError));
                                                }
                                            }}
                                        >
                                            {isDisabled ? <ShieldOff className="size-4 shrink-0" /> : <Shield className="size-4 shrink-0 text-emerald-600" />}
                                            <span className="truncate">{modelName}</span>
                                            {isDisabled ? <Badge variant="outline" className="ml-auto text-xs text-destructive">已禁用</Badge> : null}
                                        </button>
                                    );
                                });
                            })()}
                        </div>
                    )}

                    <DialogFooter>
                        <Button variant="outline" className="rounded-xl" onClick={() => setDisabledModelsSiteId(null)}>关闭</Button>
                    </DialogFooter>
                </DialogContent>
            </Dialog>
        </div>
    );
}









