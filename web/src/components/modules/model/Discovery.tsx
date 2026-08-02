'use client';

import { useMemo, useState } from 'react';
import { AlertCircle, SearchX } from 'lucide-react';
import { useTranslations } from 'next-intl';
import {
    type CatalogProvisionRequest,
    useDiscoveredModels,
    useProvisionModels,
    useSyncModelCatalog,
    useUnprovisionModels,
} from '@/api/endpoints/model-catalog';
import { SettingKey, useSetSetting, useSettingValue } from '@/api/endpoints/setting';
import { toast } from '@/components/common/Toast';
import { Button } from '@/components/ui/button';
import { useSearchStore } from '@/components/modules/toolbar';
import { MapToGroupDialog, UnprovisionDialog } from './DiscoveryDialogs';
import { DiscoveryRow } from './DiscoveryRow';
import { ALL_VENDORS, DiscoveryBulkBar, DiscoveryToolbar, type StatusFilter } from './DiscoveryToolbar';

const UNKNOWN_VENDOR_KEY = 'unknown';

function errorMessage(error: unknown) {
    return error instanceof Error ? error.message : String(error);
}

/**
 * 模型发现：列出所有渠道上报的上游模型，挑选哪些建分组、哪些重映射到已有分组。
 * 自动建组关闭时，未选中的模型不会进入目录，也不会对外提供。
 */
export function ModelDiscovery() {
    const t = useTranslations('model.discovery');
    const discovered = useDiscoveredModels();
    const syncCatalog = useSyncModelCatalog();
    const provision = useProvisionModels();
    const unprovision = useUnprovisionModels();
    const setSetting = useSetSetting();
    const provisioning = useSettingValue(SettingKey.CatalogGroupProvisioning, 'manual');
    const searchTerm = useSearchStore((state) => state.getSearchTerm('model'));

    const [selected, setSelected] = useState<Set<string>>(new Set());
    const [vendorFilter, setVendorFilter] = useState<string>(ALL_VENDORS);
    const [statusFilter, setStatusFilter] = useState<StatusFilter>('all');
    const [mapDialogOpen, setMapDialogOpen] = useState(false);
    const [removeDialogOpen, setRemoveDialogOpen] = useState(false);

    const items = useMemo(() => discovered.data ?? [], [discovered.data]);
    const vendors = useMemo(() => {
        const unique = new Set(items.map((item) => item.vendor));
        return [...unique].sort((left, right) => {
            if (left === '') return 1;
            if (right === '') return -1;
            return left.localeCompare(right);
        });
    }, [items]);

    const filtered = useMemo(() => {
        const term = searchTerm.trim().toLowerCase();
        return items.filter((item) => {
            if (statusFilter !== 'all' && item.status !== statusFilter) return false;
            if (vendorFilter !== ALL_VENDORS) {
                const key = item.vendor || UNKNOWN_VENDOR_KEY;
                if (key !== vendorFilter) return false;
            }
            if (!term) return true;
            return [item.name, item.canonical_name ?? '', item.group_name ?? '', ...(item.site_names ?? [])]
                .some((value) => value.toLowerCase().includes(term));
        });
    }, [items, searchTerm, statusFilter, vendorFilter]);

    const selectedNames = useMemo(
        () => filtered.filter((item) => selected.has(item.normalized_name)).map((item) => item.name),
        [filtered, selected],
    );
    const pending = provision.isPending || unprovision.isPending;
    const allFilteredSelected = filtered.length > 0 && selectedNames.length === filtered.length;

    const toggle = (normalized: string) => {
        setSelected((current) => {
            const next = new Set(current);
            if (next.has(normalized)) next.delete(normalized);
            else next.add(normalized);
            return next;
        });
    };

    const toggleAll = () => {
        setSelected((current) => {
            if (allFilteredSelected) return new Set<string>();
            const next = new Set(current);
            filtered.forEach((item) => next.add(item.normalized_name));
            return next;
        });
    };

    const runProvision = (request: CatalogProvisionRequest, closeDialog?: () => void) => {
        provision.mutate(request, {
            onSuccess: (result) => {
                closeDialog?.();
                setSelected(new Set());
                toast.success(t('provisionDone'), {
                    description: t('provisionResult', {
                        groups: result.groups_created,
                        items: result.group_items_created,
                        aliases: result.aliases_created + result.canonicals_merged,
                        deleted: result.groups_deleted,
                    }),
                });
            },
            onError: (error) => toast.error(t('provisionFailed'), { description: errorMessage(error) }),
        });
    };

    return (
        <div className="flex h-full min-h-0 flex-col gap-3">
            <DiscoveryToolbar
                autoProvisioning={provisioning.value === 'auto'}
                autoProvisioningPending={setSetting.isPending}
                onAutoProvisioningChange={(enabled) =>
                    setSetting.mutate(
                        { key: SettingKey.CatalogGroupProvisioning, value: enabled ? 'auto' : 'manual' },
                        {
                            onError: (error) =>
                                toast.error(t('autoProvisioningFailed'), { description: errorMessage(error) }),
                        },
                    )
                }
                syncPending={syncCatalog.isPending}
                onSync={() =>
                    syncCatalog.mutate(undefined, {
                        onSuccess: (result) =>
                            toast.success(t('syncDone'), {
                                description: t('syncResult', {
                                    groups: result.groups_created,
                                    skipped: result.skipped,
                                }),
                            }),
                        onError: (error) => toast.error(t('syncFailed'), { description: errorMessage(error) }),
                    })
                }
                vendors={vendors}
                vendorFilter={vendorFilter}
                onVendorFilterChange={setVendorFilter}
                statusFilter={statusFilter}
                onStatusFilterChange={setStatusFilter}
                total={items.length}
                shown={filtered.length}
            />

            {selectedNames.length > 0 ? (
                <DiscoveryBulkBar
                    selectedCount={selectedNames.length}
                    pending={pending}
                    onCreateGroups={() => runProvision({ models: selectedNames })}
                    onMapToGroup={() => setMapDialogOpen(true)}
                    onRemove={() => setRemoveDialogOpen(true)}
                    onClear={() => setSelected(new Set())}
                />
            ) : null}

            <section
                aria-label={t('title')}
                className="min-h-0 flex-1 overflow-y-auto rounded-lg border bg-card"
            >
                {discovered.error && !discovered.data ? (
                    <div role="alert" className="m-3 rounded-md border border-destructive/30 bg-destructive/10 p-3 text-sm text-destructive">
                        <AlertCircle className="mb-2 size-4" />
                        <p>{t('loadFailed', { message: errorMessage(discovered.error) })}</p>
                        <Button type="button" variant="outline" size="sm" className="mt-3" onClick={() => void discovered.refetch()}>
                            {t('retry')}
                        </Button>
                    </div>
                ) : null}

                {filtered.length > 0 ? (
                    <>
                        <div className="sticky top-0 z-10 flex min-h-10 items-center gap-3 border-b bg-card px-3">
                            <input
                                type="checkbox"
                                checked={allFilteredSelected}
                                onChange={toggleAll}
                                aria-label={t('selectAll')}
                                className="size-4 rounded border-border bg-background align-middle accent-primary"
                            />
                            <span className="text-xs text-muted-foreground">{t('selectAll')}</span>
                        </div>
                        <ul>
                            {filtered.map((item) => (
                                <DiscoveryRow
                                    key={item.normalized_name}
                                    item={item}
                                    selected={selected.has(item.normalized_name)}
                                    onToggle={() => toggle(item.normalized_name)}
                                />
                            ))}
                        </ul>
                    </>
                ) : null}

                {!discovered.isLoading && !discovered.error && filtered.length === 0 ? (
                    <div className="grid min-h-40 place-items-center px-4 text-center text-sm text-muted-foreground">
                        <div>
                            <SearchX className="mx-auto mb-2 size-5" />
                            {items.length === 0 ? t('emptyChannels') : t('emptyFiltered')}
                        </div>
                    </div>
                ) : null}
            </section>

            <MapToGroupDialog
                open={mapDialogOpen}
                modelNames={selectedNames}
                pending={provision.isPending}
                onOpenChange={setMapDialogOpen}
                onSubmit={(payload) =>
                    runProvision(
                        {
                            models: selectedNames,
                            target_name: payload.targetName,
                            delete_empty_source_groups: payload.deleteEmptySourceGroups,
                        },
                        () => setMapDialogOpen(false),
                    )
                }
            />

            <UnprovisionDialog
                open={removeDialogOpen}
                modelNames={selectedNames}
                pending={unprovision.isPending}
                onOpenChange={setRemoveDialogOpen}
                onSubmit={(deleteGroup) =>
                    unprovision.mutate(
                        { models: selectedNames, delete_group: deleteGroup },
                        {
                            onSuccess: (result) => {
                                setRemoveDialogOpen(false);
                                setSelected(new Set());
                                toast.success(t('removeDone'), {
                                    description: t('removeResult', {
                                        groups: result.groups_deleted,
                                        items: result.group_items_removed,
                                        aliases: result.aliases_removed,
                                        canonicals: result.canonicals_removed,
                                    }),
                                });
                            },
                            onError: (error) =>
                                toast.error(t('removeFailed'), { description: errorMessage(error) }),
                        },
                    )
                }
            />
        </div>
    );
}
