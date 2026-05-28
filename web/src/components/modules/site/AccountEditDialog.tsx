'use client';

import { useCallback, useMemo, useState, type FormEvent } from 'react';
import { useTranslations } from 'next-intl';
import { CalendarCheck2, RefreshCw, UserRound, Waypoints, XIcon } from 'lucide-react';
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
import { ProxySelector } from '@/components/modules/proxy-pool/ProxySelector';
import { toast } from '@/components/common/Toast';
import { useSettingStore } from '@/stores/setting';
import {
    Site as SiteRecord,
    SiteAccount,
    SiteCredentialType,
    SitePlatform,
    useCreateSiteAccount,
    useUpdateSiteAccount,
} from '@/api/endpoints/site';
import type { ProxyMode } from '@/api/endpoints/proxy-pool';
import { translateSiteMessage } from './site-message';

type SiteAccountFormState = {
    site_id: number;
    name: string;
    credential_type: SiteCredentialType;
    username: string;
    password: string;
    access_token: string;
    api_key: string;
    refresh_token: string;
    token_expires_at: string;
    platform_user_id: string;
    proxy_mode: ProxyMode;
    proxy_config_id: number | null;
    enabled: boolean;
    auto_sync: boolean;
    auto_checkin: boolean;
    random_checkin: boolean;
    checkin_interval_hours: number;
    checkin_random_window_minutes: number;
};

const CREDENTIAL_LABELS: Record<SiteCredentialType, string> = {
    [SiteCredentialType.UsernamePassword]: '用户名 / 密码',
    [SiteCredentialType.AccessToken]: 'Access Token',
    [SiteCredentialType.APIKey]: 'API Key',
};

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
        refresh_token: '',
        token_expires_at: '',
        platform_user_id: '',
        proxy_mode: 'inherit',
        proxy_config_id: null,
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
        refresh_token: account.refresh_token ?? '',
        token_expires_at:
            account.token_expires_at > 0 ? String(account.token_expires_at) : '',
        platform_user_id: account.platform_user_id
            ? String(account.platform_user_id)
            : '',
        proxy_mode: account.proxy_mode ?? 'inherit',
        proxy_config_id: account.proxy_config_id ?? null,
        enabled: account.enabled,
        auto_sync: account.auto_sync,
        auto_checkin: account.auto_checkin,
        random_checkin: account.random_checkin,
        checkin_interval_hours: account.checkin_interval_hours,
        checkin_random_window_minutes: account.checkin_random_window_minutes,
    };
}

function parseTokenExpiresAtInput(value: string) {
    const trimmed = value.trim();
    if (!trimmed) {
        return 0;
    }
    if (/^\d+$/.test(trimmed)) {
        const parsed = Number(trimmed);
        if (!Number.isFinite(parsed) || parsed <= 0) {
            throw new Error('token_expires_at 必须是正整数时间戳');
        }
        return parsed < 1_000_000_000_000 ? Math.trunc(parsed * 1000) : Math.trunc(parsed);
    }
    const parsed = Date.parse(trimmed);
    if (!Number.isFinite(parsed) || parsed <= 0) {
        throw new Error('token_expires_at 必须是时间戳或可解析时间');
    }
    return Math.trunc(parsed);
}

function getErrorMessage(error: unknown) {
    if (error instanceof Error) return error.message;
    if (typeof error === 'object' && error !== null && 'message' in error) {
        const message = (error as { message?: unknown }).message;
        if (typeof message === 'string') return message;
    }
    return '操作失败';
}

interface AccountEditDialogProps {
    open: boolean;
    onOpenChange: (open: boolean) => void;
    site: SiteRecord | null;
    account: SiteAccount | null;
}

/**
 * 站点账号编辑/创建弹窗。视觉风格与 Channel/Group 卡片编辑面板（MorphingDialog）
 * 保持一致：bg-card / rounded-3xl / text-2xl 标题 / 自定义 close 按钮。
 * 内部 flex 布局并对长表单提供独立滚动区域，确保视口高度较小时底部按钮可点击。
 */
export function AccountEditDialog({ open, onOpenChange, site, account }: AccountEditDialogProps) {
    const t = useTranslations();
    const tProxy = useTranslations('proxyPool');
    const locale = useSettingStore((state) => state.locale);
    const createSiteAccount = useCreateSiteAccount();
    const updateSiteAccount = useUpdateSiteAccount();
    const [accountForm, setAccountForm] = useState<SiteAccountFormState | null>(() => {
        if (account) return createAccountForm(account);
        if (site) return createEmptyAccountForm(site);
        return null;
    });

    const currentPlatform = site?.platform ?? SitePlatform.NewAPI;
    const currentCredentialOptions = useMemo(
        () => credentialOptions(currentPlatform),
        [currentPlatform],
    );

    const handleSubmit = useCallback(
        async (event: FormEvent<HTMLFormElement>) => {
            event.preventDefault();
            if (!site || !accountForm) {
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
            if (
                accountForm.credential_type === SiteCredentialType.AccessToken &&
                !accountForm.access_token.trim()
            ) {
                toast.error('请输入 Access Token');
                return;
            }
            if (
                accountForm.credential_type === SiteCredentialType.APIKey &&
                !accountForm.api_key.trim()
            ) {
                toast.error('请输入 API Key');
                return;
            }
            if (accountForm.auto_checkin && accountForm.random_checkin) {
                if (
                    !Number.isFinite(accountForm.checkin_interval_hours) ||
                    accountForm.checkin_interval_hours < 1 ||
                    accountForm.checkin_interval_hours > 720
                ) {
                    toast.error('最小签到间隔必须在 1 到 720 小时之间');
                    return;
                }
                if (
                    !Number.isFinite(accountForm.checkin_random_window_minutes) ||
                    accountForm.checkin_random_window_minutes < 0 ||
                    accountForm.checkin_random_window_minutes > 1440
                ) {
                    toast.error('随机延迟窗口必须在 0 到 1440 分钟之间');
                    return;
                }
            }

            const parsedPlatformUserID = accountForm.platform_user_id.trim()
                ? Number(accountForm.platform_user_id.trim())
                : null;
            if (
                parsedPlatformUserID !== null &&
                (!Number.isInteger(parsedPlatformUserID) || parsedPlatformUserID <= 0)
            ) {
                toast.error('Platform User ID 必须是大于 0 的整数');
                return;
            }

            let parsedTokenExpiresAt = 0;
            try {
                parsedTokenExpiresAt = parseTokenExpiresAtInput(accountForm.token_expires_at);
            } catch (error) {
                toast.error(translateSiteMessage(locale, getErrorMessage(error), t));
                return;
            }

            const trimmedAccessToken =
                accountForm.credential_type === SiteCredentialType.AccessToken
                    ? accountForm.access_token.trim()
                    : '';
            const trimmedAPIKey =
                accountForm.credential_type === SiteCredentialType.APIKey
                    ? accountForm.api_key.trim()
                    : '';

            if (accountForm.proxy_mode === 'pool' && !accountForm.proxy_config_id) {
                toast.error(tProxy('selectRequired'));
                return;
            }

            const payload = {
                site_id: accountForm.site_id,
                name: accountForm.name.trim(),
                credential_type: accountForm.credential_type,
                username: accountForm.username.trim(),
                password: accountForm.password.trim(),
                access_token: trimmedAccessToken,
                api_key: trimmedAPIKey,
                refresh_token: accountForm.refresh_token.trim(),
                token_expires_at: parsedTokenExpiresAt,
                platform_user_id: parsedPlatformUserID,
                proxy_mode: accountForm.proxy_mode,
                proxy_config_id:
                    accountForm.proxy_mode === 'pool' ? accountForm.proxy_config_id : null,
                enabled: accountForm.enabled,
                auto_sync: accountForm.auto_sync,
                auto_checkin: accountForm.auto_checkin,
                random_checkin: accountForm.random_checkin,
                checkin_interval_hours: Math.max(
                    1,
                    Math.trunc(accountForm.checkin_interval_hours || 24),
                ),
                checkin_random_window_minutes: Math.max(
                    0,
                    Math.trunc(accountForm.checkin_random_window_minutes || 0),
                ),
            };

            try {
                if (account) {
                    await updateSiteAccount.mutateAsync({ id: account.id, ...payload });
                    toast.success('站点账号已更新');
                } else {
                    await createSiteAccount.mutateAsync(payload);
                    toast.success('站点账号已创建');
                }
                onOpenChange(false);
            } catch (submitError) {
                toast.error(translateSiteMessage(locale, getErrorMessage(submitError), t));
            }
        },
        [
            site,
            account,
            accountForm,
            tProxy,
            updateSiteAccount,
            createSiteAccount,
            onOpenChange,
            locale,
            t,
        ],
    );

    const isPending = createSiteAccount.isPending || updateSiteAccount.isPending;

    if (!accountForm) {
        return (
            <Dialog open={open} onOpenChange={onOpenChange}>
                <DialogContent className="max-w-md rounded-3xl">
                    <p className="text-sm text-muted-foreground">站点上下文不存在。</p>
                </DialogContent>
            </Dialog>
        );
    }

    return (
        <Dialog open={open} onOpenChange={onOpenChange}>
            <DialogContent
                showCloseButton={false}
                className="w-screen max-w-full md:max-w-3xl bg-card text-card-foreground px-6 py-4 rounded-3xl flex flex-col gap-0 border-0 sm:max-w-3xl h-[min(calc(100vh-2rem),52rem)] overflow-hidden"
            >
                <header className="mb-4 flex items-start justify-between gap-4 shrink-0">
                    <div className="min-w-0 flex-1">
                        <h2 className="text-2xl font-bold text-card-foreground truncate">
                            {account ? '编辑站点账号' : '新增站点账号'}
                        </h2>
                        <p className="mt-1 text-sm text-muted-foreground">
                            {site
                                ? `账号会挂载到站点「${site.name}」下，并按同步结果自动投影 channel。`
                                : '配置站点账号后，可自动同步分组、模型和托管渠道。'}
                        </p>
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
                    <div className="flex-1 min-h-0 space-y-5 overflow-y-auto pr-1">
                        <div className="grid gap-4 md:grid-cols-2">
                            <label className="grid gap-2 text-sm">
                                <span className="font-medium">账号名称</span>
                                <Input
                                    value={accountForm.name}
                                    onChange={(event) =>
                                        setAccountForm((current) =>
                                            current
                                                ? { ...current, name: event.target.value }
                                                : current,
                                        )
                                    }
                                    placeholder="例如：主账号"
                                    className="rounded-xl"
                                />
                            </label>

                            <label className="grid gap-2 text-sm">
                                <span className="font-medium">凭据类型</span>
                                <Select
                                    value={accountForm.credential_type}
                                    onValueChange={(value) =>
                                        setAccountForm((current) => {
                                            if (!current) return current;
                                            const nextType = value as SiteCredentialType;
                                            return {
                                                ...current,
                                                credential_type: nextType,
                                                access_token:
                                                    nextType === SiteCredentialType.AccessToken
                                                        ? current.access_token
                                                        : '',
                                                api_key:
                                                    nextType === SiteCredentialType.APIKey
                                                        ? current.api_key
                                                        : '',
                                            };
                                        })
                                    }
                                >
                                    <SelectTrigger className="w-full rounded-xl">
                                        <SelectValue />
                                    </SelectTrigger>
                                    <SelectContent>
                                        {currentCredentialOptions.map((value) => (
                                            <SelectItem key={value} value={value}>
                                                {CREDENTIAL_LABELS[value]}
                                            </SelectItem>
                                        ))}
                                    </SelectContent>
                                </Select>
                            </label>
                        </div>

                        {accountForm.credential_type === SiteCredentialType.UsernamePassword ? (
                            <div className="grid gap-4 md:grid-cols-2">
                                <label className="grid gap-2 text-sm">
                                    <span className="font-medium">用户名</span>
                                    <Input
                                        value={accountForm.username}
                                        onChange={(event) =>
                                            setAccountForm((current) =>
                                                current
                                                    ? { ...current, username: event.target.value }
                                                    : current,
                                            )
                                        }
                                        placeholder="请输入用户名"
                                        className="rounded-xl"
                                    />
                                </label>

                                <label className="grid gap-2 text-sm">
                                    <span className="font-medium">密码</span>
                                    <Input
                                        type="password"
                                        value={accountForm.password}
                                        onChange={(event) =>
                                            setAccountForm((current) =>
                                                current
                                                    ? { ...current, password: event.target.value }
                                                    : current,
                                            )
                                        }
                                        placeholder="请输入密码"
                                        className="rounded-xl"
                                    />
                                </label>
                            </div>
                        ) : null}

                        {accountForm.credential_type === SiteCredentialType.AccessToken ? (
                            <div className="grid gap-4">
                                <label className="grid gap-2 text-sm">
                                    <span className="font-medium">Access Token</span>
                                    <Input
                                        value={accountForm.access_token}
                                        onChange={(event) =>
                                            setAccountForm((current) =>
                                                current
                                                    ? { ...current, access_token: event.target.value }
                                                    : current,
                                            )
                                        }
                                        placeholder="请输入 Access Token"
                                        className="rounded-xl"
                                    />
                                </label>

                                {currentPlatform === SitePlatform.Sub2API ? (
                                    <div className="grid gap-2">
                                        <div className="grid gap-4 md:grid-cols-2">
                                            <label className="grid gap-2 text-sm">
                                                <span className="font-medium">Refresh Token</span>
                                                <Input
                                                    value={accountForm.refresh_token}
                                                    onChange={(event) =>
                                                        setAccountForm((current) =>
                                                            current
                                                                ? {
                                                                      ...current,
                                                                      refresh_token: event.target.value,
                                                                  }
                                                                : current,
                                                        )
                                                    }
                                                    placeholder="可选：请输入 refresh_token"
                                                    className="rounded-xl"
                                                />
                                            </label>

                                            <label className="grid gap-2 text-sm">
                                                <span className="font-medium">token_expires_at</span>
                                                <Input
                                                    value={accountForm.token_expires_at}
                                                    onChange={(event) =>
                                                        setAccountForm((current) =>
                                                            current
                                                                ? {
                                                                      ...current,
                                                                      token_expires_at: event.target.value,
                                                                  }
                                                                : current,
                                                        )
                                                    }
                                                    placeholder="可选：F12 中的时间戳或时间字符串"
                                                    className="rounded-xl"
                                                />
                                            </label>
                                        </div>
                                        <span className="text-xs text-muted-foreground">
                                            Sub2API 推荐同时填写 F12 里的 <code>refresh_token</code>{' '}
                                            与 <code>token_expires_at</code>，会在快过期或 401
                                            时自动续期。
                                        </span>
                                    </div>
                                ) : null}
                            </div>
                        ) : null}

                        {accountForm.credential_type === SiteCredentialType.APIKey ? (
                            <label className="grid gap-2 text-sm">
                                <span className="font-medium">API Key</span>
                                <Input
                                    value={accountForm.api_key}
                                    onChange={(event) =>
                                        setAccountForm((current) =>
                                            current
                                                ? { ...current, api_key: event.target.value }
                                                : current,
                                        )
                                    }
                                    placeholder="请输入 API Key"
                                    className="rounded-xl"
                                />
                            </label>
                        ) : null}

                        {currentPlatform === SitePlatform.NewAPI ? (
                            <label className="grid gap-2 text-sm">
                                <span className="font-medium">Platform User ID</span>
                                <Input
                                    value={accountForm.platform_user_id}
                                    onChange={(event) =>
                                        setAccountForm((current) =>
                                            current
                                                ? { ...current, platform_user_id: event.target.value }
                                                : current,
                                        )
                                    }
                                    placeholder="可选：例如 11494"
                                    className="rounded-xl"
                                />
                                <span className="text-xs text-muted-foreground">
                                    部分 New API 站点同步 token、分组和签到时要求额外提供用户
                                    ID。导入数据会尽量自动填充该值。
                                </span>
                            </label>
                        ) : null}

                        <div className="grid gap-2 text-sm">
                            <ProxySelector
                                allowInherit
                                value={{
                                    proxy_mode: accountForm.proxy_mode,
                                    proxy_config_id: accountForm.proxy_config_id,
                                }}
                                onChange={(next) =>
                                    setAccountForm((current) =>
                                        current
                                            ? {
                                                  ...current,
                                                  proxy_mode: next.proxy_mode,
                                                  proxy_config_id: next.proxy_config_id ?? null,
                                              }
                                            : current,
                                    )
                                }
                            />
                            <span className="text-xs text-muted-foreground">
                                用于该账号的同步、签到和模型拉取；自动投影的 channel 会跟随这里解析后的代理。
                            </span>
                        </div>

                        <div className="grid gap-4 md:grid-cols-2">
                            <div className="flex items-center justify-between rounded-2xl border border-border/60 bg-muted/20 px-4 py-3">
                                <div>
                                    <div className="flex items-center gap-2">
                                        <UserRound className="size-4 text-muted-foreground" />
                                        <span className="text-sm font-medium">启用账号</span>
                                    </div>
                                    <div className="text-xs text-muted-foreground">
                                        停用后不参与同步和签到
                                    </div>
                                </div>
                                <Switch
                                    checked={accountForm.enabled}
                                    onCheckedChange={(checked) =>
                                        setAccountForm((current) =>
                                            current ? { ...current, enabled: checked } : current,
                                        )
                                    }
                                />
                            </div>

                            <div className="flex items-center justify-between rounded-2xl border border-border/60 bg-muted/20 px-4 py-3">
                                <div>
                                    <div className="flex items-center gap-2">
                                        <RefreshCw className="size-4 text-muted-foreground" />
                                        <span className="text-sm font-medium">自动同步</span>
                                    </div>
                                    <div className="text-xs text-muted-foreground">
                                        定时拉取分组、模型和 key
                                    </div>
                                </div>
                                <Switch
                                    checked={accountForm.auto_sync}
                                    onCheckedChange={(checked) =>
                                        setAccountForm((current) =>
                                            current ? { ...current, auto_sync: checked } : current,
                                        )
                                    }
                                />
                            </div>

                            <div className="flex items-center justify-between rounded-2xl border border-border/60 bg-muted/20 px-4 py-3">
                                <div>
                                    <div className="flex items-center gap-2">
                                        <CalendarCheck2 className="size-4 text-muted-foreground" />
                                        <span className="text-sm font-medium">自动签到</span>
                                    </div>
                                    <div className="text-xs text-muted-foreground">
                                        定时执行平台签到
                                    </div>
                                </div>
                                <Switch
                                    checked={accountForm.auto_checkin}
                                    onCheckedChange={(checked) =>
                                        setAccountForm((current) =>
                                            current
                                                ? { ...current, auto_checkin: checked }
                                                : current,
                                        )
                                    }
                                />
                            </div>

                            <div className="flex items-center justify-between rounded-2xl border border-border/60 bg-muted/20 px-4 py-3">
                                <div>
                                    <div className="flex items-center gap-2">
                                        <CalendarCheck2 className="size-4 text-muted-foreground" />
                                        <span className="text-sm font-medium">随机签到</span>
                                    </div>
                                    <div className="text-xs text-muted-foreground">
                                        在间隔基础上添加随机延迟
                                    </div>
                                </div>
                                <Switch
                                    checked={accountForm.random_checkin}
                                    onCheckedChange={(checked) =>
                                        setAccountForm((current) =>
                                            current
                                                ? { ...current, random_checkin: checked }
                                                : current,
                                        )
                                    }
                                />
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
                                        onChange={(event) =>
                                            setAccountForm((current) =>
                                                current
                                                    ? {
                                                          ...current,
                                                          checkin_interval_hours: Number(event.target.value),
                                                      }
                                                    : current,
                                            )
                                        }
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
                                        onChange={(event) =>
                                            setAccountForm((current) =>
                                                current
                                                    ? {
                                                          ...current,
                                                          checkin_random_window_minutes: Number(
                                                              event.target.value,
                                                          ),
                                                      }
                                                    : current,
                                            )
                                        }
                                        placeholder="120"
                                        className="rounded-xl"
                                    />
                                </label>
                            </div>
                        ) : null}

                        <div className="grid gap-3 rounded-2xl border border-border/60 bg-muted/10 p-4 text-sm text-muted-foreground">
                            <div className="flex items-center gap-2">
                                <Waypoints className="size-4" />
                                <span>投影规则说明</span>
                            </div>
                            <p>
                                同步后会以 <code>site_user_group</code> 为主维度生成托管
                                channel；无分组时使用 <code>default</code>。同一分组下的多个
                                key 会聚合到同一个 channel；多端点兼容站点会按模型已归属的
                                请求端点格式继续拆成独立托管 channel。
                            </p>
                            {accountForm.auto_checkin && accountForm.random_checkin ? (
                                <p>
                                    随机签到会基于&ldquo;上次成功签到时间 + 最小间隔 + 0
                                    到随机延迟窗口&rdquo;的规则生成下次执行时间，适合需要接近 24
                                    小时间隔的站点。
                                </p>
                            ) : null}
                        </div>
                    </div>

                    <footer className="mt-5 flex flex-col-reverse gap-2 sm:flex-row sm:justify-end shrink-0 border-t border-border/60 pt-4">
                        <Button
                            type="button"
                            variant="outline"
                            className="rounded-xl"
                            onClick={() => onOpenChange(false)}
                        >
                            取消
                        </Button>
                        <Button
                            type="submit"
                            className="rounded-xl"
                            disabled={isPending}
                        >
                            {isPending ? '保存中...' : '保存'}
                        </Button>
                    </footer>
                </form>
            </DialogContent>
        </Dialog>
    );
}
