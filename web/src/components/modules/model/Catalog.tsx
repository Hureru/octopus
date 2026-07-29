'use client';

import { useMemo, useState } from 'react';
import { RefreshCw, SearchX } from 'lucide-react';
import { useTranslations } from 'next-intl';
import { useChannelList } from '@/api/endpoints/channel';
import { type CanonicalModel, useModelCatalog, useSyncModelCatalog } from '@/api/endpoints/model-catalog';
import { useSiteList } from '@/api/endpoints/site';
import { toast } from '@/components/common/Toast';
import { Button } from '@/components/ui/button';
import { useSearchStore } from '@/components/modules/toolbar';
import { cn } from '@/lib/utils';
import { CatalogDetail } from './CatalogDetail';

function errorMessage(error: unknown) {
    return error instanceof Error ? error.message : String(error);
}

export function ModelCatalog() {
    const t = useTranslations('model.catalog');
    const catalog = useModelCatalog();
    const syncCatalog = useSyncModelCatalog();
    const channels = useChannelList();
    const sites = useSiteList();
    const searchTerm = useSearchStore((state) => state.getSearchTerm('model'));
    const [selectedId, setSelectedId] = useState<number | null>(null);

    const filtered = useMemo(() => {
        const term = searchTerm.trim().toLowerCase();
        return (catalog.data ?? []).filter((item) => {
            if (!term) return true;
            return [
                item.name,
                item.normalized_name,
                ...item.aliases.map((alias) => alias.alias),
                ...item.route_candidates.map((candidate) => candidate.upstream_model_name),
            ].some((value) => value.toLowerCase().includes(term));
        });
    }, [catalog.data, searchTerm]);

    const effectiveSelectedId = filtered.some((item) => item.id === selectedId)
        ? selectedId
        : filtered[0]?.id ?? null;
    const selected = filtered.find((item) => item.id === effectiveSelectedId) ?? null;
    const channelNameById = useMemo(
        () => new Map((channels.data ?? []).map((item) => [item.raw.id, item.raw.name])),
        [channels.data],
    );
    const siteNameById = useMemo(
        () => new Map((sites.data ?? []).map((site) => [site.id, site.name])),
        [sites.data],
    );
    const accountNameById = useMemo(
        () => new Map((sites.data ?? []).flatMap((site) => site.accounts.map((account) => [account.id, account.name] as const))),
        [sites.data],
    );

    const sync = () => {
        syncCatalog.mutate(undefined, {
            onSuccess: (result) =>
                toast.success(t('syncComplete'), {
                    description: t('syncResult', {
                        canonical: result.canonical_created,
                        candidates: result.candidates_created + result.candidates_updated,
                    }),
                }),
            onError: (error) => toast.error(t('syncFailed'), { description: errorMessage(error) }),
        });
    };

    return (
        <div className="grid h-full min-h-0 grid-rows-[minmax(12rem,2fr)_minmax(0,3fr)] gap-3 md:grid-cols-[18rem_minmax(0,1fr)] md:grid-rows-1 md:gap-4">
            <aside
                aria-label={t('title')}
                className="flex min-h-0 flex-col overflow-hidden rounded-lg border bg-card"
            >
                <div className="flex items-center justify-between gap-2 border-b px-3 py-2">
                    <div>
                        <h2 className="text-sm font-semibold">{t('title')}</h2>
                        <p className="text-xs tabular-nums text-muted-foreground">
                            {t('modelCount', { count: filtered.length })}
                        </p>
                    </div>
                    <Button
                        type="button"
                        variant="ghost"
                        size="icon"
                        aria-label={t('sync')}
                        title={t('sync')}
                        onClick={sync}
                        disabled={syncCatalog.isPending}
                    >
                        <RefreshCw className={cn(syncCatalog.isPending && 'animate-spin')} />
                    </Button>
                </div>
                <div className="min-h-0 flex-1 overflow-y-auto p-1.5">
                    {filtered.map((model) => (
                        <CatalogListItem
                            key={model.id}
                            model={model}
                            selected={model.id === effectiveSelectedId}
                            onSelect={() => setSelectedId(model.id)}
                        />
                    ))}
                    {!catalog.isLoading && filtered.length === 0 ? (
                        <div className="grid min-h-40 place-items-center px-4 text-center text-sm text-muted-foreground">
                            <div>
                                <SearchX className="mx-auto mb-2 size-5" />
                                {t('empty')}
                            </div>
                        </div>
                    ) : null}
                </div>
            </aside>

            <section
                aria-label={selected?.name ?? t('title')}
                className="min-h-0 overflow-y-auto rounded-lg border bg-card"
            >
                {selected ? (
                    <CatalogDetail
                        key={selected.id}
                        canonical={selected}
                        channelNameById={channelNameById}
                        siteNameById={siteNameById}
                        accountNameById={accountNameById}
                    />
                ) : (
                    <div className="grid min-h-64 place-items-center text-sm text-muted-foreground">
                        {catalog.isLoading ? t('loading') : t('empty')}
                    </div>
                )}
            </section>
        </div>
    );
}

function CatalogListItem({
    model,
    selected,
    onSelect,
}: {
    model: CanonicalModel;
    selected: boolean;
    onSelect: () => void;
}) {
    return (
        <button
            type="button"
            aria-pressed={selected}
            onClick={onSelect}
            className={cn(
                'mb-1 min-h-12 w-full rounded-md px-3 py-2 text-left transition-colors',
                selected ? 'bg-primary text-primary-foreground' : 'hover:bg-muted',
            )}
        >
            <span className="block truncate text-sm font-medium">{model.name}</span>
            <span className={cn('block text-xs', selected ? 'text-primary-foreground/75' : 'text-muted-foreground')}>
                {model.route_candidates.length} / {model.aliases.length}
            </span>
        </button>
    );
}
