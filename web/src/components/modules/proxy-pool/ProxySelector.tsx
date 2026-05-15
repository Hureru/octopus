'use client';

import type { ProxyMode } from '@/api/endpoints/proxy-pool';
import { useProxyConfigurationList } from '@/api/endpoints/proxy-pool';
import { Button } from '@/components/ui/button';
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select';
import { useProxyPoolDialogStore } from './dialog-store';

type ProxyValue = {
    proxy_mode: ProxyMode;
    proxy_config_id?: number | null;
};

type ProxySelectorProps = {
    value: ProxyValue;
    onChange: (value: ProxyValue) => void;
    allowInherit?: boolean;
    disabled?: boolean;
};

const MODE_LABELS: Record<ProxyMode, string> = {
    inherit: '继承站点设置',
    direct: '直连',
    system: '系统代理',
    pool: '代理池',
};

export function ProxySelector({ value, onChange, allowInherit = false, disabled = false }: ProxySelectorProps) {
    const { data: proxies = [], isLoading } = useProxyConfigurationList();
    const openProxyPool = useProxyPoolDialogStore((state) => state.open);
    const selectedProxy = proxies.find((item) => item.id === value.proxy_config_id) ?? null;
    const enabledProxies = proxies.filter((item) => item.enabled || item.id === value.proxy_config_id);
    const mode = value.proxy_mode || (allowInherit ? 'inherit' : 'direct');

    const modes: ProxyMode[] = allowInherit
        ? ['inherit', 'direct', 'system', 'pool']
        : ['direct', 'system', 'pool'];
    const noEnabledProxy = proxies.every((item) => !item.enabled);

    return (
        <div className="space-y-2">
            <div className="grid gap-2 md:grid-cols-2">
                <div className="space-y-2">
                    <label className="text-sm font-medium text-card-foreground">代理模式</label>
                    <Select
                        value={mode}
                        disabled={disabled}
                        onValueChange={(nextMode) => {
                            const proxy_mode = nextMode as ProxyMode;
                            onChange({
                                proxy_mode,
                                proxy_config_id: proxy_mode === 'pool' ? value.proxy_config_id ?? null : null,
                            });
                        }}
                    >
                        <SelectTrigger className="w-full rounded-xl">
                            <SelectValue />
                        </SelectTrigger>
                        <SelectContent className="rounded-xl">
                            {modes.map((item) => (
                                <SelectItem key={item} className="rounded-xl" value={item}>
                                    {MODE_LABELS[item]}
                                </SelectItem>
                            ))}
                        </SelectContent>
                    </Select>
                </div>

                {mode === 'pool' ? (
                    <div className="space-y-2">
                        <label className="text-sm font-medium text-card-foreground">代理配置</label>
                        {enabledProxies.length > 0 ? (
                            <Select
                                value={value.proxy_config_id ? String(value.proxy_config_id) : ''}
                                disabled={disabled || isLoading}
                                onValueChange={(proxyId) => onChange({ proxy_mode: 'pool', proxy_config_id: Number(proxyId) })}
                            >
                                <SelectTrigger className="w-full rounded-xl">
                                    <SelectValue placeholder="选择代理配置" />
                                </SelectTrigger>
                                <SelectContent className="rounded-xl">
                                    {enabledProxies.map((proxy) => (
                                        <SelectItem key={proxy.id} className="rounded-xl" value={String(proxy.id)} disabled={!proxy.enabled}>
                                            {proxy.name}{!proxy.enabled ? '（已停用）' : ''}
                                        </SelectItem>
                                    ))}
                                </SelectContent>
                            </Select>
                        ) : null}
                        {selectedProxy && !selectedProxy.enabled ? (
                            <div className="rounded-xl border border-destructive/30 bg-destructive/10 px-3 py-2 text-xs text-destructive">
                                当前选择的代理配置已停用，运行时会失败；保存前请改选可用代理。
                            </div>
                        ) : null}
                        {noEnabledProxy ? (
                            <div className="flex items-center justify-between gap-2 rounded-xl border border-border/70 bg-muted/20 px-3 py-2 text-sm">
                                <span className="text-muted-foreground">
                                    {proxies.length === 0 ? '代理池为空' : '没有可用的代理配置'}
                                </span>
                                <Button type="button" size="sm" variant="outline" className="rounded-xl" onClick={openProxyPool}>
                                    打开代理池管理
                                </Button>
                            </div>
                        ) : (
                            <Button type="button" size="sm" variant="ghost" className="rounded-xl text-xs text-muted-foreground" onClick={openProxyPool}>
                                打开代理池管理
                            </Button>
                        )}
                    </div>
                ) : null}
            </div>
        </div>
    );
}
