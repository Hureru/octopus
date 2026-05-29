'use client';

import {
    useCallback,
    useLayoutEffect,
    useMemo,
    useRef,
    useState,
    type FormEvent,
    type ReactNode,
} from 'react';
import { useTranslations } from 'next-intl';
import { CalendarCheck2, RefreshCw, UserRound, XIcon } from 'lucide-react';
import { AnimatePresence, motion, type Transition } from 'motion/react';
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

const FORM_SECTION_TRANSITION: Transition = {
    duration: 0.2,
    ease: 'easeOut',
};

function AnimatedFormSection({ children }: { children: ReactNode }) {
    const contentRef = useRef<HTMLDivElement>(null);
    const [height, setHeight] = useState<number | 'auto'>('auto');

    const updateHeight = useCallback(() => {
        const node = contentRef.current;
        if (!node) return;

        const nextHeight = node.offsetHeight;
        setHeight((current) => (current === nextHeight ? current : nextHeight));
    }, []);

    useLayoutEffect(() => {
        updateHeight();
    });

    useLayoutEffect(() => {
        const node = contentRef.current;
        if (!node || typeof ResizeObserver === 'undefined') return;

        const observer = new ResizeObserver(updateHeight);
        observer.observe(node);
        return () => observer.disconnect();
    }, [updateHeight]);

    return (
        <motion.div
            initial={false}
            animate={{ height }}
            transition={FORM_SECTION_TRANSITION}
            className="-mx-1 mb-4 overflow-hidden"
        >
            <div ref={contentRef} className="px-1 pb-1">{children}</div>
        </motion.div>
    );
}

function defaultCredentialType(): SiteCredentialType {
    return SiteCredentialType.AccessToken;
}

function credentialOptions(platform: SitePlatform) {
    switch (platform) {
        case SitePlatform.Sub2API:
            return [SiteCredentialType.AccessToken, SiteCredentialType.APIKey];
        case SitePlatform.OpenAI:
        case SitePlatform.Claude:
        case SitePlatform.Gemini:
            return [SiteCredentialType.AccessToken, SiteCredentialType.APIKey];
        default:
            return [
                SiteCredentialType.AccessToken,
                SiteCredentialType.UsernamePassword,
                SiteCredentialType.APIKey,
            ];
    }
}

function createEmptyAccountForm(site: SiteRecord): SiteAccountFormState {
    return {
        site_id: site.id,
        name: '',
        credential_type: defaultCredentialType(),
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

            const shouldIncludePlatformUserID =
                currentPlatform === SitePlatform.NewAPI &&
                accountForm.credential_type === SiteCredentialType.AccessToken;
            const platformUserIDInput = shouldIncludePlatformUserID
                ? accountForm.platform_user_id.trim()
                : '';
            if (shouldIncludePlatformUserID && !platformUserIDInput) {
                toast.error('请输入 Platform User ID');
                return;
            }

            const parsedPlatformUserID = platformUserIDInput
                ? Number(platformUserIDInput)
                : null;
            if (
                shouldIncludePlatformUserID &&
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
            const isUsernamePassword =
                accountForm.credential_type === SiteCredentialType.UsernamePassword;
            const isAccessToken =
                accountForm.credential_type === SiteCredentialType.AccessToken;

            if (accountForm.proxy_mode === 'pool' && !accountForm.proxy_config_id) {
                toast.error(tProxy('selectRequired'));
                return;
            }

            const payload = {
                site_id: accountForm.site_id,
                name: accountForm.name.trim(),
                credential_type: accountForm.credential_type,
                username: isUsernamePassword ? accountForm.username.trim() : '',
                password: isUsernamePassword ? accountForm.password.trim() : '',
                access_token: trimmedAccessToken,
                api_key: trimmedAPIKey,
                refresh_token: isAccessToken ? accountForm.refresh_token.trim() : '',
                token_expires_at: isAccessToken ? parsedTokenExpiresAt : 0,
                platform_user_id: shouldIncludePlatformUserID ? parsedPlatformUserID : null,
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
            currentPlatform,
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
                className="w-screen max-w-full md:max-w-xl bg-card text-card-foreground px-6 py-4 rounded-3xl flex flex-col gap-0 border-0 sm:max-w-xl max-h-[min(calc(100vh-2rem),52rem)] overflow-hidden"
            >
                <header className="mb-4 flex items-start justify-between gap-4 shrink-0">
                    <div className="min-w-0 flex-1">
                        <h2 className="text-2xl font-bold text-card-foreground truncate">
                            {account ? '编辑站点账号' : '新增站点账号'}
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
                                                platform_user_id:
                                                    nextType === SiteCredentialType.AccessToken &&
                                                    currentPlatform === SitePlatform.NewAPI
                                                        ? current.platform_user_id
                                                        : '',
                                            };
                                        })
                                    }
                                >
                                    <SelectTrigger className="w-full rounded-xl">
                                        <SelectValue />
                                    </SelectTrigger>
                                    <SelectContent className="rounded-xl">
                                        {currentCredentialOptions.map((value) => (
                                            <SelectItem className="rounded-xl" key={value} value={value}>
                                                {CREDENTIAL_LABELS[value]}
                                            </SelectItem>
                                        ))}
                                    </SelectContent>
                                </Select>
                            </label>
                        </div>

                        <AnimatedFormSection>
                            <AnimatePresence initial={false} mode="popLayout">
                                {accountForm.credential_type === SiteCredentialType.UsernamePassword ? (
                                    <motion.div
                                        key={SiteCredentialType.UsernamePassword}
                                        initial={{ opacity: 0, y: -6 }}
                                        animate={{ opacity: 1, y: 0 }}
                                        exit={{ opacity: 0, y: -6 }}
                                        transition={FORM_SECTION_TRANSITION}
                                    >
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
                                    </motion.div>
                                ) : accountForm.credential_type === SiteCredentialType.AccessToken ? (
                                    <motion.div
                                        key={SiteCredentialType.AccessToken}
                                        initial={{ opacity: 0, y: -6 }}
                                        animate={{ opacity: 1, y: 0 }}
                                        exit={{ opacity: 0, y: -6 }}
                                        transition={FORM_SECTION_TRANSITION}
                                    >
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

                                            {currentPlatform === SitePlatform.NewAPI ? (
                                                <label className="grid gap-2 text-sm">
                                                    <span className="font-medium">Platform User ID</span>
                                                    <Input
                                                        value={accountForm.platform_user_id}
                                                        onChange={(event) =>
                                                            setAccountForm((current) =>
                                                                current
                                                                    ? {
                                                                          ...current,
                                                                          platform_user_id: event.target.value,
                                                                      }
                                                                    : current,
                                                            )
                                                        }
                                                        placeholder="例如 11494"
                                                        className="rounded-xl"
                                                        required
                                                    />
                                                    <span className="text-xs text-muted-foreground">
                                                        New API 站点同步 token、分组和签到时需要用户
                                                        ID。导入数据会尽量自动填充该值。
                                                    </span>
                                                </label>
                                            ) : null}
                                        </div>
                                    </motion.div>
                                ) : (
                                    <motion.div
                                        key={SiteCredentialType.APIKey}
                                        initial={{ opacity: 0, y: -6 }}
                                        animate={{ opacity: 1, y: 0 }}
                                        exit={{ opacity: 0, y: -6 }}
                                        transition={FORM_SECTION_TRANSITION}
                                    >
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
                                    </motion.div>
                                )}
                            </AnimatePresence>
                        </AnimatedFormSection>

                        <div className="rounded-xl border border-border/50 bg-muted/20 p-4">
                            <div className="grid gap-x-6 gap-y-3 md:grid-cols-2">
                                <label className="flex cursor-pointer items-center justify-between gap-3">
                                    <span className="flex items-center gap-2 text-sm font-medium text-card-foreground">
                                        <UserRound className="size-4 text-muted-foreground" />
                                        启用账号
                                    </span>
                                    <Switch
                                        checked={accountForm.enabled}
                                        onCheckedChange={(checked) =>
                                            setAccountForm((current) =>
                                                current ? { ...current, enabled: checked } : current,
                                            )
                                        }
                                    />
                                </label>
                                <label className="flex cursor-pointer items-center justify-between gap-3">
                                    <span className="flex items-center gap-2 text-sm text-card-foreground">
                                        <RefreshCw className="size-4 text-muted-foreground" />
                                        自动同步
                                    </span>
                                    <Switch
                                        checked={accountForm.auto_sync}
                                        onCheckedChange={(checked) =>
                                            setAccountForm((current) =>
                                                current ? { ...current, auto_sync: checked } : current,
                                            )
                                        }
                                    />
                                </label>
                                <label className="flex cursor-pointer items-center justify-between gap-3">
                                    <span className="flex items-center gap-2 text-sm text-card-foreground">
                                        <CalendarCheck2 className="size-4 text-muted-foreground" />
                                        自动签到
                                    </span>
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
                                </label>
                                <label className="flex cursor-pointer items-center justify-between gap-3">
                                    <span className="flex items-center gap-2 text-sm text-card-foreground">
                                        <CalendarCheck2 className="size-4 text-muted-foreground" />
                                        随机签到
                                    </span>
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
                                </label>
                            </div>

                            <AnimatePresence initial={false}>
                                {accountForm.auto_checkin && accountForm.random_checkin ? (
                                    <motion.div
                                        key="random-checkin-options"
                                        initial={{ height: 0, opacity: 0 }}
                                        animate={{ height: 'auto', opacity: 1 }}
                                        exit={{ height: 0, opacity: 0 }}
                                        transition={FORM_SECTION_TRANSITION}
                                        className="overflow-hidden"
                                    >
                                        <motion.div
                                            initial={{ y: -6 }}
                                            animate={{ y: 0 }}
                                            exit={{ y: -6 }}
                                            transition={FORM_SECTION_TRANSITION}
                                            className="mt-4 grid gap-4 border-t border-border/50 pt-4 md:grid-cols-2"
                                        >
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
                                        </motion.div>
                                    </motion.div>
                                ) : null}
                            </AnimatePresence>
                        </div>

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
                                用于该账号的同步、签到和模型拉取；自动投影的渠道会跟随这里解析后的代理。
                            </span>
                        </div>
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
                            {isPending ? '保存中...' : account ? '保存修改' : '创建账号'}
                        </Button>
                    </footer>
                </form>
            </DialogContent>
        </Dialog>
    );
}
