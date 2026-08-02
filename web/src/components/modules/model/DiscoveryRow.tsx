'use client';

import { useTranslations } from 'next-intl';
import type { DiscoveredModel } from '@/api/endpoints/model-catalog';
import { Badge } from '@/components/ui/badge';
import { cn } from '@/lib/utils';
import { VendorBadge } from './VendorBadge';

/**
 * 模型发现列表的一行：勾选框、模型名与来源、厂商标签、归属状态。
 * 行高固定，避免筛选切换时列表跳动。
 */
export function DiscoveryRow({
    item,
    selected,
    onToggle,
}: {
    item: DiscoveredModel;
    selected: boolean;
    onToggle: () => void;
}) {
    const t = useTranslations('model.discovery');

    return (
        <li
            className={cn(
                'flex min-h-14 items-center gap-3 border-b px-3 py-2 last:border-b-0',
                selected ? 'bg-primary/5' : 'hover:bg-muted/50',
            )}
        >
            <input
                type="checkbox"
                checked={selected}
                onChange={onToggle}
                aria-label={item.name}
                className="size-4 shrink-0 rounded border-border bg-background align-middle accent-primary"
            />
            <div className="min-w-0 flex-1">
                <p className="truncate text-sm font-medium">{item.name}</p>
                <p className="truncate text-xs text-muted-foreground">
                    {t('channelCount', { count: item.channel_count })}
                    {item.site_names?.length ? ` · ${item.site_names.join(', ')}` : ''}
                    {item.status === 'mapped' && item.canonical_name
                        ? ` · ${t('mappedTo', { target: item.canonical_name })}`
                        : ''}
                </p>
            </div>
            <VendorBadge vendor={item.vendor} unknownLabel={t('unknownVendor')} className="shrink-0" />
            <StatusBadge status={item.status} />
        </li>
    );
}

function StatusBadge({ status }: { status: DiscoveredModel['status'] }) {
    const t = useTranslations('model.discovery');
    if (status === 'grouped') {
        return (
            <Badge variant="secondary" className="w-16 shrink-0 justify-center border-transparent">
                {t('statusGrouped')}
            </Badge>
        );
    }
    if (status === 'mapped') {
        return (
            <Badge variant="outline" className="w-16 shrink-0 justify-center">
                {t('statusMapped')}
            </Badge>
        );
    }
    return (
        <Badge variant="outline" className="w-16 shrink-0 justify-center border-dashed text-muted-foreground">
            {t('statusUngrouped')}
        </Badge>
    );
}
