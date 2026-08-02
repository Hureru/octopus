'use client';

import { useTranslations } from 'next-intl';
import { Layers, Link2, RefreshCw, Unlink } from 'lucide-react';
import type { DiscoveredModelStatus } from '@/api/endpoints/model-catalog';
import { Button } from '@/components/ui/button';
import {
    Select,
    SelectContent,
    SelectItem,
    SelectTrigger,
    SelectValue,
} from '@/components/ui/select';
import { cn } from '@/lib/utils';
import { vendorOption } from './vendor-options';

export type StatusFilter = DiscoveredModelStatus | 'all';

export const ALL_VENDORS = 'all';

/**
 * 顶部工具条：自动建组开关 + 目录同步 + 厂商/状态筛选。
 */
export function DiscoveryToolbar({
    autoProvisioning,
    autoProvisioningPending,
    onAutoProvisioningChange,
    syncPending,
    onSync,
    vendors,
    vendorFilter,
    onVendorFilterChange,
    statusFilter,
    onStatusFilterChange,
    total,
    shown,
}: {
    autoProvisioning: boolean;
    autoProvisioningPending: boolean;
    onAutoProvisioningChange: (enabled: boolean) => void;
    syncPending: boolean;
    onSync: () => void;
    vendors: string[];
    vendorFilter: string;
    onVendorFilterChange: (vendor: string) => void;
    statusFilter: StatusFilter;
    onStatusFilterChange: (status: StatusFilter) => void;
    total: number;
    shown: number;
}) {
    const t = useTranslations('model.discovery');

    return (
        <div className="flex shrink-0 flex-wrap items-center gap-2 rounded-lg border bg-card px-3 py-2">
            <p className="text-xs tabular-nums text-muted-foreground">
                {t('counter', { shown, total })}
            </p>

            <div className="ms-auto flex flex-wrap items-center gap-2">
                <Select value={vendorFilter} onValueChange={onVendorFilterChange}>
                    <SelectTrigger size="sm" className="w-36" aria-label={t('vendorFilter')}>
                        <SelectValue placeholder={t('vendorFilter')} />
                    </SelectTrigger>
                    <SelectContent>
                        <SelectItem value={ALL_VENDORS}>{t('allVendors')}</SelectItem>
                        {vendors.map((vendor) => (
                            <SelectItem key={vendor || 'unknown'} value={vendor || 'unknown'}>
                                {vendorOption(vendor)?.label ?? t('unknownVendor')}
                            </SelectItem>
                        ))}
                    </SelectContent>
                </Select>

                <Select
                    value={statusFilter}
                    onValueChange={(value) => onStatusFilterChange(value as StatusFilter)}
                >
                    <SelectTrigger size="sm" className="w-32" aria-label={t('statusFilter')}>
                        <SelectValue placeholder={t('statusFilter')} />
                    </SelectTrigger>
                    <SelectContent>
                        <SelectItem value="all">{t('statusAll')}</SelectItem>
                        <SelectItem value="ungrouped">{t('statusUngrouped')}</SelectItem>
                        <SelectItem value="grouped">{t('statusGrouped')}</SelectItem>
                        <SelectItem value="mapped">{t('statusMapped')}</SelectItem>
                    </SelectContent>
                </Select>

                <label className="flex min-h-8 items-center gap-2 rounded-md border px-2.5 text-xs text-muted-foreground">
                    <input
                        type="checkbox"
                        checked={autoProvisioning}
                        disabled={autoProvisioningPending}
                        onChange={(event) => onAutoProvisioningChange(event.target.checked)}
                        className="size-4 rounded border-border bg-background align-middle accent-primary disabled:cursor-not-allowed disabled:opacity-50"
                    />
                    <span title={t('autoProvisioningHint')}>{t('autoProvisioning')}</span>
                </label>

                <Button
                    type="button"
                    variant="ghost"
                    size="icon"
                    aria-label={t('sync')}
                    title={t('sync')}
                    onClick={onSync}
                    disabled={syncPending}
                >
                    <RefreshCw className={cn(syncPending && 'animate-spin')} />
                </Button>
            </div>
        </div>
    );
}

/**
 * 批量操作条：仅在有选中项时出现，高度固定避免列表抖动。
 */
export function DiscoveryBulkBar({
    selectedCount,
    pending,
    onCreateGroups,
    onMapToGroup,
    onRemove,
    onClear,
}: {
    selectedCount: number;
    pending: boolean;
    onCreateGroups: () => void;
    onMapToGroup: () => void;
    onRemove: () => void;
    onClear: () => void;
}) {
    const t = useTranslations('model.discovery');

    return (
        <div className="flex min-h-12 shrink-0 flex-wrap items-center gap-2 rounded-lg border bg-card px-3 py-2">
            <p className="text-sm tabular-nums">{t('selected', { count: selectedCount })}</p>
            <div className="ms-auto flex flex-wrap items-center gap-2">
                <Button type="button" size="sm" disabled={pending} onClick={onCreateGroups}>
                    <Layers className="size-4" />
                    {t('createGroups')}
                </Button>
                <Button type="button" size="sm" variant="outline" disabled={pending} onClick={onMapToGroup}>
                    <Link2 className="size-4" />
                    {t('mapToGroup')}
                </Button>
                <Button type="button" size="sm" variant="outline" disabled={pending} onClick={onRemove}>
                    <Unlink className="size-4" />
                    {t('remove')}
                </Button>
                <Button type="button" size="sm" variant="ghost" disabled={pending} onClick={onClear}>
                    {t('clearSelection')}
                </Button>
            </div>
        </div>
    );
}
