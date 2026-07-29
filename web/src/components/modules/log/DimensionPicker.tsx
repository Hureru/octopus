'use client';

import { useDeferredValue, useState } from 'react';
import { Check, ChevronLeft, ChevronRight, Search } from 'lucide-react';
import { useTranslations } from 'next-intl';
import {
    type UsageAnalyticsFilters,
    type UsageDimension,
    type UsageDimensionOption,
    useUsageAnalyticsDimensions,
} from '@/api/endpoints/log-analytics';
import { Button } from '@/components/ui/button';
import { Input } from '@/components/ui/input';
import { Popover, PopoverContent, PopoverTrigger } from '@/components/ui/popover';
import { cn } from '@/lib/utils';

export function DimensionPicker({
    filters,
    dimension,
    selected,
    selectedLabel,
    onSelect,
}: {
    filters: UsageAnalyticsFilters;
    dimension: UsageDimension;
    selected: number | string | null;
    selectedLabel?: string;
    onSelect: (option: UsageDimensionOption | null) => void;
}) {
    const t = useTranslations('log.analytics');
    const [open, setOpen] = useState(false);
    const [search, setSearch] = useState('');
    const [page, setPage] = useState(1);
    const deferredSearch = useDeferredValue(search);
    const query = useUsageAnalyticsDimensions(filters, dimension, deferredSearch, page, 20, open);
    const valueSelected = (option: UsageDimensionOption) =>
        typeof selected === 'number' ? option.id === selected : option.name === selected;

    return (
        <Popover
            open={open}
            onOpenChange={(next) => {
                setOpen(next);
                if (!next) {
                    setSearch('');
                    setPage(1);
                }
            }}
        >
            <PopoverTrigger asChild>
                <button
                    type="button"
                    className={cn(
                        'flex h-8 max-w-52 items-center gap-1.5 rounded-md border border-border/70 bg-background px-2 text-[11px] text-foreground',
                        selected == null && 'text-muted-foreground',
                    )}
                    aria-label={t(`filters.${dimension}`)}
                >
                    <span className="truncate">{selectedLabel || t(`filters.all_${dimension}`)}</span>
                </button>
            </PopoverTrigger>
            <PopoverContent align="start" className="w-72 rounded-lg p-2">
                <div className="relative">
                    <Search className="pointer-events-none absolute left-2.5 top-1/2 size-3.5 -translate-y-1/2 text-muted-foreground" />
                    <Input
                        value={search}
                        onChange={(event) => {
                            setSearch(event.target.value);
                            setPage(1);
                        }}
                        aria-label={t('filters.search')}
                        placeholder={t('filters.search')}
                        className="h-8 pl-8 text-xs"
                    />
                </div>
                <div className="mt-2 max-h-64 overflow-y-auto">
                    <button
                        type="button"
                        onClick={() => {
                            onSelect(null);
                            setOpen(false);
                        }}
                        className="flex min-h-9 w-full items-center gap-2 rounded-md px-2 text-left text-xs hover:bg-muted"
                    >
                        <Check className={cn('size-3.5 shrink-0', selected == null ? 'opacity-100' : 'opacity-0')} />
                        <span className="min-w-0 flex-1 truncate">{t(`filters.all_${dimension}`)}</span>
                    </button>
                    {(query.data?.items ?? []).map((option) => (
                        <button
                            key={`${option.id}:${option.name}`}
                            type="button"
                            onClick={() => {
                                onSelect(option);
                                setOpen(false);
                            }}
                            className="flex min-h-9 w-full items-center gap-2 rounded-md px-2 text-left text-xs hover:bg-muted"
                        >
                            <Check className={cn('size-3.5 shrink-0', valueSelected(option) ? 'opacity-100' : 'opacity-0')} />
                            <span className="min-w-0 flex-1 truncate">{option.name}</span>
                        </button>
                    ))}
                    {query.error ? (
                        <div role="alert" className="px-2 py-8 text-center text-xs text-destructive">
                            {query.error instanceof Error ? query.error.message : String(query.error)}
                        </div>
                    ) : !query.isLoading && (query.data?.items ?? []).length === 0 ? (
                        <div className="px-2 py-8 text-center text-xs text-muted-foreground">{t('filters.noOptions')}</div>
                    ) : null}
                </div>
                <div className="mt-2 flex items-center justify-between border-t pt-2">
                    <span className="text-[11px] text-muted-foreground">{t('breakdown.page', { page })}</span>
                    <div className="flex gap-1">
                        <Button
                            type="button"
                            variant="ghost"
                            size="icon-sm"
                            aria-label={t('breakdown.previous')}
                            disabled={page <= 1}
                            onClick={() => setPage((current) => Math.max(1, current - 1))}
                        >
                            <ChevronLeft />
                        </Button>
                        <Button
                            type="button"
                            variant="ghost"
                            size="icon-sm"
                            aria-label={t('breakdown.next')}
                            disabled={!query.data?.has_more}
                            onClick={() => setPage((current) => current + 1)}
                        >
                            <ChevronRight />
                        </Button>
                    </div>
                </div>
            </PopoverContent>
        </Popover>
    );
}
