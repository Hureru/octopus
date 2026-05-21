'use client';

import { useMemo, useState } from 'react';
import { useTranslations } from 'next-intl';
import { CalendarIcon, Check, ChevronDown, Filter, Search, X } from 'lucide-react';
import type { Matcher } from 'react-day-picker';
import dayjs from 'dayjs';
import { Popover, PopoverContent, PopoverTrigger } from '@/components/ui/popover';
import { Calendar } from '@/components/ui/calendar';
import { Badge } from '@/components/ui/badge';
import { buttonVariants } from '@/components/ui/button';
import { cn } from '@/lib/utils';
import { useChannelList } from '@/api/endpoints/channel';
import { useSiteChannelList } from '@/api/endpoints/site-channel';
import { SettingKey, useSettingValue } from '@/api/endpoints/setting';
import { useToolbarViewOptionsStore } from '@/components/modules/toolbar/view-options-store';

type ChannelEntry = {
    id: number;
    name: string;
};

type ChannelGroup = {
    key: string;
    label: string;
    channels: ChannelEntry[];
};

function unixToDate(value: number | undefined) {
    if (!value) return undefined;
    return new Date(value * 1000);
}

function formatDateTime(value: number | undefined) {
    if (!value) return '';
    return dayjs(value * 1000).format('YYYY-MM-DD HH:mm');
}

interface DateTimePickerProps {
    value?: number;
    placeholder: string;
    defaultTime: 'start' | 'end';
    disabledRange: Matcher[];
    onChange: (value: number | undefined) => void;
}

function DateTimePicker({ value, placeholder, defaultTime, disabledRange, onChange }: DateTimePickerProps) {
    const t = useTranslations('toolbar');
    const [open, setOpen] = useState(false);
    const selectedDate = unixToDate(value);
    const timeString = value
        ? dayjs(value * 1000).format('HH:mm')
        : defaultTime === 'start'
            ? '00:00'
            : '23:59';

    const handleDateSelect = (date: Date | undefined) => {
        if (!date) {
            onChange(undefined);
            return;
        }
        const [h, m] = timeString.split(':').map((n) => Number.parseInt(n, 10));
        const next = dayjs(date).hour(h || 0).minute(m || 0).second(defaultTime === 'end' ? 59 : 0);
        onChange(Math.floor(next.valueOf() / 1000));
    };

    const handleTimeChange = (next: string) => {
        if (!selectedDate) return;
        const [h, m] = next.split(':').map((n) => Number.parseInt(n, 10));
        const updated = dayjs(selectedDate)
            .hour(Number.isFinite(h) ? h : 0)
            .minute(Number.isFinite(m) ? m : 0)
            .second(defaultTime === 'end' ? 59 : 0);
        onChange(Math.floor(updated.valueOf() / 1000));
    };

    const handleClear = (event: React.MouseEvent) => {
        event.stopPropagation();
        onChange(undefined);
    };

    return (
        <Popover open={open} onOpenChange={setOpen}>
            <PopoverTrigger asChild>
                <button
                    type="button"
                    className={cn(
                        'flex h-9 w-full items-center gap-2 rounded-lg border border-border bg-muted/20 px-2.5 text-xs transition-colors hover:bg-muted/30',
                        !value && 'text-muted-foreground',
                    )}
                >
                    <CalendarIcon className="size-3.5 shrink-0 text-muted-foreground" />
                    <span className="flex-1 truncate text-left tabular-nums">
                        {value ? formatDateTime(value) : placeholder}
                    </span>
                    {value ? (
                        <span
                            role="button"
                            tabIndex={0}
                            aria-label={t('popover.logFilter.date.clear')}
                            onClick={handleClear}
                            onKeyDown={(event) => {
                                if (event.key === 'Enter' || event.key === ' ') {
                                    event.preventDefault();
                                    event.stopPropagation();
                                    onChange(undefined);
                                }
                            }}
                            className="-mr-1 flex size-4 shrink-0 items-center justify-center rounded text-muted-foreground hover:bg-muted/60 hover:text-foreground focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
                        >
                            <X className="size-3" />
                        </span>
                    ) : null}
                </button>
            </PopoverTrigger>
            <PopoverContent
                align="start"
                side="bottom"
                sideOffset={6}
                className="w-auto rounded-2xl border border-border/60 bg-card p-0 shadow-xl"
            >
                <Calendar
                    mode="single"
                    selected={selectedDate}
                    onSelect={handleDateSelect}
                    disabled={disabledRange}
                    numberOfMonths={1}
                />
                <div className="flex items-center gap-2 border-t border-border/60 px-3 py-2">
                    <span className="text-[11px] font-medium text-muted-foreground">HH:mm</span>
                    <input
                        type="time"
                        value={timeString}
                        onChange={(e) => handleTimeChange(e.target.value)}
                        disabled={!selectedDate}
                        className="h-7 flex-1 rounded-md border border-border bg-background px-2 text-xs tabular-nums outline-none focus:ring-1 focus:ring-ring disabled:cursor-not-allowed disabled:opacity-50"
                    />
                </div>
            </PopoverContent>
        </Popover>
    );
}

export function LogFilterPopover() {
    const t = useTranslations('toolbar');
    const logDateRange = useToolbarViewOptionsStore((s) => s.logDateRange);
    const logChannelIds = useToolbarViewOptionsStore((s) => s.logChannelIds);
    const setLogDateRange = useToolbarViewOptionsStore((s) => s.setLogDateRange);
    const setLogChannelIds = useToolbarViewOptionsStore((s) => s.setLogChannelIds);
    const { value: logKeepPeriodValue } = useSettingValue(SettingKey.RelayLogKeepPeriod, '0');
    const { data: channels } = useChannelList();
    const { data: sites } = useSiteChannelList({ includeHistory: false });

    const [search, setSearch] = useState('');
    const [expanded, setExpanded] = useState<Set<string>>(new Set(['__manual__']));

    const logKeepPeriod = Number.parseInt(logKeepPeriodValue, 10) || 0;

    const groups = useMemo<ChannelGroup[]>(() => {
        if (!channels) return [];
        const siteNameById = new Map<number, string>();
        for (const site of sites ?? []) siteNameById.set(site.site_id, site.site_name);

        const siteBuckets = new Map<number, ChannelEntry[]>();
        const manualBucket: ChannelEntry[] = [];

        for (const item of channels) {
            const entry: ChannelEntry = { id: item.raw.id, name: item.raw.name };
            const src = item.raw.managed_source;
            if (item.raw.managed && src?.site_id) {
                const list = siteBuckets.get(src.site_id) ?? [];
                list.push(entry);
                siteBuckets.set(src.site_id, list);
            } else {
                manualBucket.push(entry);
            }
        }

        const result: ChannelGroup[] = [];
        for (const [siteId, list] of siteBuckets) {
            list.sort((a, b) => a.name.localeCompare(b.name));
            result.push({
                key: `site:${siteId}`,
                label: siteNameById.get(siteId) ?? t('popover.logFilter.channel.siteFallback', { id: siteId }),
                channels: list,
            });
        }
        result.sort((a, b) => a.label.localeCompare(b.label));
        if (manualBucket.length > 0) {
            manualBucket.sort((a, b) => a.name.localeCompare(b.name));
            result.push({
                key: '__manual__',
                label: t('popover.logFilter.channel.manualGroup'),
                channels: manualBucket,
            });
        }
        return result;
    }, [channels, sites, t]);

    const filteredGroups = useMemo(() => {
        const term = search.trim().toLowerCase();
        if (!term) return groups;
        return groups
            .map((g) => {
                const matchesGroup = g.label.toLowerCase().includes(term);
                const matchedChannels = matchesGroup
                    ? g.channels
                    : g.channels.filter((c) => c.name.toLowerCase().includes(term));
                return { ...g, channels: matchedChannels };
            })
            .filter((g) => g.channels.length > 0);
    }, [groups, search]);

    const selectedSet = useMemo(() => new Set(logChannelIds), [logChannelIds]);

    const toggleChannel = (id: number) => {
        const next = new Set(selectedSet);
        if (next.has(id)) next.delete(id);
        else next.add(id);
        setLogChannelIds(Array.from(next));
    };

    const toggleGroup = (group: ChannelGroup) => {
        const ids = group.channels.map((c) => c.id);
        const allSelected = ids.every((id) => selectedSet.has(id));
        const next = new Set(selectedSet);
        if (allSelected) ids.forEach((id) => next.delete(id));
        else ids.forEach((id) => next.add(id));
        setLogChannelIds(Array.from(next));
    };

    const toggleExpanded = (key: string) => {
        const next = new Set(expanded);
        if (next.has(key)) next.delete(key);
        else next.add(key);
        setExpanded(next);
    };

    const handleClear = () => {
        setLogDateRange({});
        setLogChannelIds([]);
        setSearch('');
    };

    const startDisabled = useMemo<Matcher[]>(() => {
        const matchers: Matcher[] = [{ after: new Date() }];
        if (logKeepPeriod > 0) {
            matchers.push({ before: dayjs().subtract(logKeepPeriod, 'day').startOf('day').toDate() });
        }
        if (logDateRange.end) {
            matchers.push({ after: new Date(logDateRange.end * 1000) });
        }
        return matchers;
    }, [logKeepPeriod, logDateRange.end]);

    const endDisabled = useMemo<Matcher[]>(() => {
        const matchers: Matcher[] = [{ after: new Date() }];
        if (logKeepPeriod > 0) {
            matchers.push({ before: dayjs().subtract(logKeepPeriod, 'day').startOf('day').toDate() });
        }
        if (logDateRange.start) {
            matchers.push({ before: new Date(logDateRange.start * 1000) });
        }
        return matchers;
    }, [logKeepPeriod, logDateRange.start]);

    const handleStartChange = (value: number | undefined) => {
        setLogDateRange({ ...logDateRange, start: value });
    };

    const handleEndChange = (value: number | undefined) => {
        setLogDateRange({ ...logDateRange, end: value });
    };

    const dateActive = !!logDateRange.start || !!logDateRange.end;
    const activeCount = (dateActive ? 1 : 0) + (logChannelIds.length > 0 ? 1 : 0);

    return (
        <Popover>
            <PopoverTrigger asChild>
                <button
                    type="button"
                    aria-label={t('popover.logFilter.title')}
                    className={cn(
                        buttonVariants({
                            variant: 'ghost',
                            size: 'icon',
                            className: 'relative rounded-xl transition-none hover:bg-transparent text-muted-foreground hover:text-foreground',
                        }),
                    )}
                >
                    <Filter className="size-4 transition-colors duration-300" />
                    {activeCount > 0 ? (
                        <span className="absolute -right-0.5 -top-0.5 inline-flex h-4 min-w-4 items-center justify-center rounded-full bg-primary px-1 text-[10px] font-semibold leading-none text-primary-foreground">
                            {activeCount}
                        </span>
                    ) : null}
                </button>
            </PopoverTrigger>
            <PopoverContent
                align="end"
                side="bottom"
                sideOffset={8}
                className="w-[20rem] rounded-2xl border border-border/60 bg-card p-3 shadow-xl"
            >
                <div className="flex flex-col gap-3">
                    <div className="flex items-center justify-between">
                        <p className="text-sm font-semibold">{t('popover.logFilter.title')}</p>
                        <button
                            type="button"
                            onClick={handleClear}
                            disabled={activeCount === 0}
                            className="text-xs text-muted-foreground transition-colors hover:text-foreground disabled:cursor-not-allowed disabled:opacity-50"
                        >
                            {t('popover.logFilter.clearAll')}
                        </button>
                    </div>

                    <div className="grid gap-2">
                        <p className="text-xs font-medium text-muted-foreground">{t('popover.logFilter.date.title')}</p>
                        <div className="grid grid-cols-1 gap-2">
                            <DateTimePicker
                                value={logDateRange.start}
                                placeholder={t('popover.logFilter.date.startPlaceholder')}
                                defaultTime="start"
                                disabledRange={startDisabled}
                                onChange={handleStartChange}
                            />
                            <DateTimePicker
                                value={logDateRange.end}
                                placeholder={t('popover.logFilter.date.endPlaceholder')}
                                defaultTime="end"
                                disabledRange={endDisabled}
                                onChange={handleEndChange}
                            />
                        </div>
                        {logKeepPeriod > 0 ? (
                            <p className="text-[11px] leading-4 text-muted-foreground">{t('popover.logFilter.date.hint')}</p>
                        ) : null}
                    </div>

                    <div className="grid gap-2">
                        <div className="flex items-center justify-between">
                            <p className="text-xs font-medium text-muted-foreground">{t('popover.logFilter.channel.title')}</p>
                            {logChannelIds.length > 0 ? (
                                <Badge variant="secondary" className="h-5 px-1.5 text-[10px] font-semibold tabular-nums">
                                    {logChannelIds.length}
                                </Badge>
                            ) : null}
                        </div>
                        <div className="flex h-8 items-center gap-2 rounded-lg border border-border bg-muted/20 px-2">
                            <Search className="size-3.5 shrink-0 text-muted-foreground" />
                            <input
                                type="text"
                                value={search}
                                onChange={(e) => setSearch(e.target.value)}
                                placeholder={t('popover.logFilter.channel.searchPlaceholder')}
                                className="w-full bg-transparent text-xs outline-none placeholder:text-muted-foreground"
                            />
                        </div>
                        <div className="max-h-60 overflow-auto rounded-lg border border-border/60">
                            {filteredGroups.length === 0 ? (
                                <p className="px-3 py-4 text-center text-xs text-muted-foreground">
                                    {t('popover.logFilter.channel.empty')}
                                </p>
                            ) : (
                                filteredGroups.map((group) => {
                                    const ids = group.channels.map((c) => c.id);
                                    const selectedInGroup = ids.filter((id) => selectedSet.has(id)).length;
                                    const allSelected = selectedInGroup === ids.length;
                                    const isExpanded = expanded.has(group.key) || !!search.trim();

                                    return (
                                        <div key={group.key} className="border-b border-border/40 last:border-b-0">
                                            <div className="flex items-center gap-1 px-2 py-1.5 hover:bg-muted/30">
                                                <button
                                                    type="button"
                                                    onClick={() => toggleExpanded(group.key)}
                                                    className="flex size-5 items-center justify-center rounded text-muted-foreground hover:text-foreground"
                                                >
                                                    <ChevronDown
                                                        className={cn('size-3.5 transition-transform', isExpanded ? '' : '-rotate-90')}
                                                    />
                                                </button>
                                                <button
                                                    type="button"
                                                    onClick={() => toggleGroup(group)}
                                                    className="flex flex-1 items-center gap-2 text-left"
                                                >
                                                    <span
                                                        className={cn(
                                                            'flex size-3.5 shrink-0 items-center justify-center rounded border transition-colors',
                                                            allSelected
                                                                ? 'border-primary bg-primary text-primary-foreground'
                                                                : selectedInGroup > 0
                                                                    ? 'border-primary/60 bg-primary/30'
                                                                    : 'border-border',
                                                        )}
                                                    >
                                                        {allSelected ? <Check className="size-2.5" /> : selectedInGroup > 0 ? <span className="size-1.5 rounded-sm bg-primary" /> : null}
                                                    </span>
                                                    <span className="flex-1 truncate text-xs font-semibold text-foreground">
                                                        {group.label}
                                                    </span>
                                                    <span className="text-[10px] tabular-nums text-muted-foreground">
                                                        {selectedInGroup}/{ids.length}
                                                    </span>
                                                </button>
                                            </div>
                                            {isExpanded ? (
                                                <div className="flex flex-col">
                                                    {group.channels.map((channel) => {
                                                        const checked = selectedSet.has(channel.id);
                                                        return (
                                                            <button
                                                                key={channel.id}
                                                                type="button"
                                                                onClick={() => toggleChannel(channel.id)}
                                                                className="flex items-center gap-2 px-2 py-1.5 pl-8 text-left hover:bg-muted/40"
                                                            >
                                                                <span
                                                                    className={cn(
                                                                        'flex size-3.5 shrink-0 items-center justify-center rounded border transition-colors',
                                                                        checked
                                                                            ? 'border-primary bg-primary text-primary-foreground'
                                                                            : 'border-border',
                                                                    )}
                                                                >
                                                                    {checked ? <Check className="size-2.5" /> : null}
                                                                </span>
                                                                <span className="flex-1 truncate text-xs text-foreground">{channel.name}</span>
                                                            </button>
                                                        );
                                                    })}
                                                </div>
                                            ) : null}
                                        </div>
                                    );
                                })
                            )}
                        </div>
                    </div>
                </div>
            </PopoverContent>
        </Popover>
    );
}
