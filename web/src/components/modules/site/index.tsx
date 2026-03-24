'use client';

import { useState, type FormEvent, type ReactNode } from 'react';
import {
    Site as SiteRecord,
    SiteAccount,
    SiteCredentialType,
    SitePlatform,
    type CustomHeader,
    useCheckinAllSites,
    useCheckinSiteAccount,
    useCreateSite,
    useCreateSiteAccount,
    useDeleteSite,
    useDeleteSiteAccount,
    useEnableSite,
    useEnableSiteAccount,
    useSiteList,
    useSyncAllSites,
    useSyncSiteAccount,
    useUpdateSite,
    useUpdateSiteAccount,
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
import {
    CalendarCheck2,
    Cable,
    CircleAlert,
    FolderTree,
    Globe2,
    KeyRound,
    Link2,
    Pencil,
    Plus,
    RefreshCw,
    Sparkles,
    Trash2,
    UserRound,
    Waypoints,
    X,
} from 'lucide-react';

type SiteFormState = {
    name: string;
    platform: SitePlatform;
    base_url: string;
    enabled: boolean;
    proxy: boolean;
    site_proxy: string;
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
    enabled: boolean;
    auto_sync: boolean;
    auto_checkin: boolean;
    random_checkin: boolean;
    checkin_interval_hours: number;
    checkin_random_window_minutes: number;
};

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

function createEmptySiteForm(): SiteFormState {
    return {
        name: '',
        platform: SitePlatform.NewAPI,
        base_url: '',
        enabled: true,
        proxy: false,
        site_proxy: '',
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
    return account.channel_bindings.map((binding) => `${binding.group_key} -> #${binding.channel_id}`);
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

    const [siteDialogOpen, setSiteDialogOpen] = useState(false);
    const [editingSite, setEditingSite] = useState<SiteRecord | null>(null);
    const [siteForm, setSiteForm] = useState<SiteFormState>(createEmptySiteForm());

    const [accountDialogOpen, setAccountDialogOpen] = useState(false);
    const [accountSite, setAccountSite] = useState<SiteRecord | null>(null);
    const [editingAccount, setEditingAccount] = useState<SiteAccount | null>(null);
    const [accountForm, setAccountForm] = useState<SiteAccountFormState | null>(null);

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

        const customHeader = trimHeaders(siteForm.custom_header);
        const invalidHeader = customHeader.find((item) => !item.header_key || !item.header_value);
        if (invalidHeader) {
            toast.error('自定义 Header 的键和值都不能为空');
            return;
        }

        const payload = {
            name: siteForm.name.trim(),
            platform: siteForm.platform,
            base_url: siteForm.base_url.trim(),
            enabled: siteForm.enabled,
            proxy: siteForm.proxy,
            site_proxy: siteForm.site_proxy.trim(),
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

        const payload = {
            site_id: accountForm.site_id,
            name: accountForm.name.trim(),
            credential_type: accountForm.credential_type,
            username: accountForm.username.trim(),
            password: accountForm.password.trim(),
            access_token: accountForm.access_token.trim(),
            api_key: accountForm.api_key.trim(),
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
        if (!window.confirm(`确认删除站点「${site.name}」及其托管渠道吗？`)) {
            return;
        }

        try {
            await deleteSite.mutateAsync(site.id);
            toast.success('站点已删除');
        } catch (deleteError) {
            toast.error(getErrorMessage(deleteError));
        }
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
        if (!window.confirm(`确认删除账号「${account.name}」及其托管渠道吗？`)) {
            return;
        }

        try {
            await deleteSiteAccount.mutateAsync(account.id);
            toast.success('站点账号已删除');
        } catch (deleteError) {
            toast.error(getErrorMessage(deleteError));
        }
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
                                维护不同平台的站点与账号，自动同步分组、模型和 key，并将每个分组投影为一个托管 channel。同分组下的多个 key 会聚合到同一个 channel。
                            </p>
                            <div className="rounded-2xl border border-border/60 bg-muted/20 px-4 py-3 text-sm text-muted-foreground">
                                当前方案以 <code>site_user_group</code> 作为 channel 拆分维度；无分组时使用 <code>default</code>。
                            </div>
                        </div>
                        <div className="flex flex-wrap gap-2">
                            <Button onClick={openCreateSiteDialog} className="rounded-xl">
                                <Plus className="size-4" />新增站点
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
                                    <h2 className="text-xl font-semibold">{site.name}</h2>
                                    <Badge variant="outline">{PLATFORM_LABELS[site.platform]}</Badge>
                                    <Badge variant="outline" className={site.enabled ? 'text-emerald-600' : 'text-muted-foreground'}>
                                        {site.enabled ? '启用中' : '已停用'}
                                    </Badge>
                                    <Badge variant="outline">{site.accounts.length} 个账号</Badge>
                                </div>
                                <div className="flex flex-wrap items-center gap-2 text-sm text-muted-foreground">
                                    <Link2 className="size-4" />
                                    <span>{site.base_url}</span>
                                </div>
                                <div className="flex flex-wrap gap-2 text-xs">
                                    <Badge variant="outline">{site.proxy ? '启用代理' : '直连'}</Badge>
                                    {site.site_proxy ? <Badge variant="outline">{site.site_proxy}</Badge> : null}
                                    <Badge variant="outline">{site.custom_header.length} 个自定义 Header</Badge>
                                </div>
                            </div>
                            <div className="flex flex-wrap gap-2">
                                <Button variant="outline" size="sm" className="rounded-xl" onClick={() => openCreateAccountDialog(site)}>
                                    <Plus className="size-4" />新增账号
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
                                            </div>

                                            <div className="grid gap-3 sm:grid-cols-2 xl:grid-cols-4">
                                                <SiteMetric label="分组" value={account.user_groups.length} />
                                                <SiteMetric label="Key" value={account.tokens.length} />
                                                <SiteMetric label="模型" value={account.models.length} />
                                                <SiteMetric label="托管渠道" value={account.channel_bindings.length} />
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
                                <Select value={siteForm.platform} onValueChange={(value) => setSiteForm((current) => ({ ...current, platform: value as SitePlatform }))}>
                                    <SelectTrigger className="w-full rounded-xl">
                                        <SelectValue />
                                    </SelectTrigger>
                                    <SelectContent>
                                        {Object.entries(PLATFORM_LABELS).map(([value, label]) => (
                                            <SelectItem key={value} value={value}>{label}</SelectItem>
                                        ))}
                                    </SelectContent>
                                </Select>
                            </label>
                        </div>

                        <label className="grid gap-2 text-sm">
                            <span className="font-medium">站点地址</span>
                            <Input value={siteForm.base_url} onChange={(event) => setSiteForm((current) => ({ ...current, base_url: event.target.value }))} placeholder="https://example.com" className="rounded-xl" />
                        </label>

                        <div className="grid gap-4 md:grid-cols-2">
                            <div className="flex items-center justify-between rounded-2xl border border-border/60 bg-muted/20 px-4 py-3">
                                <div>
                                    <div className="text-sm font-medium">启用站点</div>
                                    <div className="text-xs text-muted-foreground">停用后不再投影托管渠道</div>
                                </div>
                                <Switch checked={siteForm.enabled} onCheckedChange={(checked) => setSiteForm((current) => ({ ...current, enabled: checked }))} />
                            </div>

                            <div className="flex items-center justify-between rounded-2xl border border-border/60 bg-muted/20 px-4 py-3">
                                <div>
                                    <div className="text-sm font-medium">使用代理</div>
                                    <div className="text-xs text-muted-foreground">请求站点接口时走代理</div>
                                </div>
                                <Switch checked={siteForm.proxy} onCheckedChange={(checked) => setSiteForm((current) => ({ ...current, proxy: checked }))} />
                            </div>
                        </div>

                        <label className="grid gap-2 text-sm">
                            <span className="font-medium">站点级代理</span>
                            <Input value={siteForm.site_proxy} onChange={(event) => setSiteForm((current) => ({ ...current, site_proxy: event.target.value }))} placeholder="可选：例如 socks5://127.0.0.1:7890" className="rounded-xl" />
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

                            <div className="grid gap-4 md:grid-cols-4">
                                <div className="flex items-center justify-between rounded-2xl border border-border/60 bg-muted/20 px-4 py-3">
                                    <div className="flex items-center gap-2"><UserRound className="size-4 text-muted-foreground" /><span className="text-sm font-medium">启用</span></div>
                                    <Switch checked={accountForm.enabled} onCheckedChange={(checked) => setAccountForm((current) => current ? { ...current, enabled: checked } : current)} />
                                </div>

                                <div className="flex items-center justify-between rounded-2xl border border-border/60 bg-muted/20 px-4 py-3">
                                    <div className="flex items-center gap-2"><RefreshCw className="size-4 text-muted-foreground" /><span className="text-sm font-medium">自动同步</span></div>
                                    <Switch checked={accountForm.auto_sync} onCheckedChange={(checked) => setAccountForm((current) => current ? { ...current, auto_sync: checked } : current)} />
                                </div>

                                <div className="flex items-center justify-between rounded-2xl border border-border/60 bg-muted/20 px-4 py-3">
                                    <div className="flex items-center gap-2"><CalendarCheck2 className="size-4 text-muted-foreground" /><span className="text-sm font-medium">自动签到</span></div>
                                    <Switch checked={accountForm.auto_checkin} onCheckedChange={(checked) => setAccountForm((current) => current ? { ...current, auto_checkin: checked } : current)} />
                                </div>

                                <div className="flex items-center justify-between rounded-2xl border border-border/60 bg-muted/20 px-4 py-3">
                                    <div className="flex items-center gap-2"><CalendarCheck2 className="size-4 text-muted-foreground" /><span className="text-sm font-medium">随机签到</span></div>
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
                                <p>同步后会以 <code>site_user_group</code> 为维度生成托管 channel；无分组时使用 <code>default</code>。同一分组下的多个 key 会聚合到同一个 channel。</p>
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
        </div>
    );
}
