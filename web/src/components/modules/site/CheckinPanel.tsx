'use client';

import { useMemo, useState, useCallback } from 'react';
import * as AccordionPrimitive from '@radix-ui/react-accordion';
import { CalendarCheck2, ChevronDownIcon, RefreshCw } from 'lucide-react';
import { Accordion, AccordionContent, AccordionItem } from '@/components/ui/accordion';
import { Badge } from '@/components/ui/badge';
import { Button } from '@/components/ui/button';
import {
    Table, TableBody, TableCell, TableHead, TableHeader, TableRow,
} from '@/components/ui/table';
import { Tooltip, TooltipContent, TooltipTrigger } from '@/components/animate-ui/components/animate/tooltip';
import { toast } from '@/components/common/Toast';
import { useCheckinSiteAccount, useCheckinAllSites, type Site } from '@/api/endpoints/site';
import { cn } from '@/lib/utils';

// --- helpers (same as index.tsx, kept local to avoid touching main page) ---

function formatDateTime(value?: string | null) {
    if (!value) return '从未执行';
    const date = new Date(value);
    if (Number.isNaN(date.getTime()) || date.getFullYear() <= 1) return '从未执行';
    return date.toLocaleString();
}

function statusLabel(status: string) {
    switch (status) {
        case 'success': return '成功';
        case 'failed':  return '失败';
        case 'skipped': return '跳过';
        default:        return '未执行';
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
        default:
            return 'border-border bg-muted/40 text-muted-foreground';
    }
}

// --- types ---

type CheckinRow = {
    siteId: number;
    siteName: string;
    accountId: number;
    accountName: string;
    status: string;
    lastCheckinAt: string | null;
    message: string;
};

type FilterStatus = 'all' | 'success' | 'failed' | 'skipped' | 'idle';

const STATUS_PRIORITY: Record<string, number> = {
    failed: 0, idle: 1, skipped: 2, success: 3,
};

const STORAGE_KEY = 'octopus-checkin-panel-expanded';

const FILTERS: { key: FilterStatus; label: string; countKey?: string }[] = [
    { key: 'all',     label: '全部' },
    { key: 'success', label: '成功' },
    { key: 'failed',  label: '失败' },
    { key: 'skipped', label: '跳过' },
    { key: 'idle',    label: '未执行' },
];

// --- component ---

export function CheckinPanel({ sites, isLoading }: { sites: Site[] | undefined; isLoading: boolean }) {
    const checkinAccount = useCheckinSiteAccount();
    const checkinAll = useCheckinAllSites();

    const [filterStatus, setFilterStatus] = useState<FilterStatus>('all');
    const [checkinLoadingIds, setCheckinLoadingIds] = useState<Set<number>>(new Set());
    const [accordionValue, setAccordionValue] = useState<string>(() => {
        if (typeof window === 'undefined') return 'checkin-panel';
        return localStorage.getItem(STORAGE_KEY) === 'collapsed' ? '' : 'checkin-panel';
    });

    const handleValueChange = useCallback((value: string) => {
        setAccordionValue(value);
        localStorage.setItem(STORAGE_KEY, value ? 'expanded' : 'collapsed');
    }, []);

    // flatten sites → rows
    const allRows = useMemo<CheckinRow[]>(() => {
        if (!sites) return [];
        const rows: CheckinRow[] = [];
        for (const site of sites) {
            for (const acc of site.accounts ?? []) {
                rows.push({
                    siteId: site.id,
                    siteName: site.name,
                    accountId: acc.id,
                    accountName: acc.name,
                    status: acc.last_checkin_status || 'idle',
                    lastCheckinAt: acc.last_checkin_at ?? null,
                    message: acc.last_checkin_message || '',
                });
            }
        }
        rows.sort((a, b) => {
            const pa = STATUS_PRIORITY[a.status] ?? 99;
            const pb = STATUS_PRIORITY[b.status] ?? 99;
            if (pa !== pb) return pa - pb;
            const ta = a.lastCheckinAt ? new Date(a.lastCheckinAt).getTime() : 0;
            const tb = b.lastCheckinAt ? new Date(b.lastCheckinAt).getTime() : 0;
            return tb - ta;
        });
        return rows;
    }, [sites]);

    const summary = useMemo(() => {
        const s = { total: 0, success: 0, failed: 0, skipped: 0, idle: 0 };
        for (const r of allRows) {
            s.total++;
            if (r.status === 'success') s.success++;
            else if (r.status === 'failed') s.failed++;
            else if (r.status === 'skipped') s.skipped++;
            else s.idle++;
        }
        return s;
    }, [allRows]);

    const filteredRows = useMemo(
        () => filterStatus === 'all' ? allRows : allRows.filter((r) => r.status === filterStatus),
        [allRows, filterStatus],
    );

    const handleCheckinRow = useCallback(async (accountId: number) => {
        setCheckinLoadingIds((prev) => new Set(prev).add(accountId));
        try {
            const res = await checkinAccount.mutateAsync(accountId);
            if (res.status === 'success') {
                toast.success(`签到成功${res.reward ? `：${res.reward}` : ''}`);
            } else {
                toast.error(`签到失败：${res.message || '未知错误'}`);
            }
        } catch (err) {
            toast.error(`签到失败：${(err as Error).message}`);
        } finally {
            setCheckinLoadingIds((prev) => { const n = new Set(prev); n.delete(accountId); return n; });
        }
    }, [checkinAccount]);

    const handleCheckinAll = useCallback(() => {
        checkinAll.mutate(undefined, {
            onSuccess: () => toast.success('已触发后台全量签到'),
            onError: (err) => toast.error(`全量签到失败：${err.message}`),
        });
    }, [checkinAll]);

    const summaryCountForFilter = (key: FilterStatus) => {
        if (key === 'all') return summary.total;
        return summary[key];
    };

    return (
        <section className="rounded-3xl border border-border bg-card text-card-foreground">
            <Accordion type="single" collapsible value={accordionValue} onValueChange={handleValueChange}>
                <AccordionItem value="checkin-panel">
                    {/* custom header: trigger + checkin-all button side by side */}
                    <AccordionPrimitive.Header className="flex items-center gap-3 px-6 py-4">
                        <AccordionPrimitive.Trigger className="flex flex-1 items-center gap-3 text-left text-sm font-medium outline-none [&[data-state=open]>svg.chevron]:rotate-180">
                            <CalendarCheck2 className="size-5 text-primary shrink-0" />
                            <span className="font-semibold">签到总览</span>
                            <div className="flex items-center gap-1.5 ml-2">
                                {summary.success > 0 && <Badge variant="outline" className="text-[11px] border-emerald-500/20 bg-emerald-500/10 text-emerald-700 dark:text-emerald-300">{summary.success} 成功</Badge>}
                                {summary.failed > 0 && <Badge variant="outline" className="text-[11px] border-destructive/20 bg-destructive/10 text-destructive">{summary.failed} 失败</Badge>}
                                {summary.skipped > 0 && <Badge variant="outline" className="text-[11px] border-amber-500/20 bg-amber-500/10 text-amber-700 dark:text-amber-300">{summary.skipped} 跳过</Badge>}
                                {summary.idle > 0 && <Badge variant="outline" className="text-[11px] border-border bg-muted/40 text-muted-foreground">{summary.idle} 未执行</Badge>}
                            </div>
                            <ChevronDownIcon className="chevron ml-auto text-muted-foreground size-4 shrink-0 transition-transform duration-200" />
                        </AccordionPrimitive.Trigger>
                        <Button
                            variant="outline"
                            size="sm"
                            className="shrink-0 rounded-xl"
                            disabled={checkinAll.isPending}
                            onClick={(e) => { e.stopPropagation(); handleCheckinAll(); }}
                        >
                            <RefreshCw className={cn('size-3.5', checkinAll.isPending && 'animate-spin')} />
                            {checkinAll.isPending ? '签到中...' : '全量签到'}
                        </Button>
                    </AccordionPrimitive.Header>

                    <AccordionContent className="px-6">
                        {/* filter bar */}
                        <div className="flex flex-wrap gap-1.5 mb-4">
                            {FILTERS.map((f) => (
                                <button
                                    key={f.key}
                                    type="button"
                                    onClick={() => setFilterStatus(f.key)}
                                    className={cn(
                                        'px-3 py-1 text-xs rounded-lg transition-colors',
                                        filterStatus === f.key
                                            ? 'bg-primary text-primary-foreground'
                                            : 'bg-muted hover:bg-muted/80 text-muted-foreground',
                                    )}
                                >
                                    {f.label} ({summaryCountForFilter(f.key)})
                                </button>
                            ))}
                        </div>

                        {/* table */}
                        <div className="max-h-[400px] overflow-y-auto rounded-xl border border-border/50">
                            {isLoading ? (
                                <div className="py-10 text-center text-sm text-muted-foreground">正在加载...</div>
                            ) : filteredRows.length === 0 ? (
                                <div className="py-10 text-center text-sm text-muted-foreground">
                                    {allRows.length === 0 ? '暂无账号数据' : '没有匹配的记录'}
                                </div>
                            ) : (
                                <Table>
                                    <TableHeader>
                                        <TableRow>
                                            <TableHead className="w-[120px]">站点</TableHead>
                                            <TableHead className="w-[120px]">账号</TableHead>
                                            <TableHead className="w-[80px]">状态</TableHead>
                                            <TableHead className="w-[160px]">时间</TableHead>
                                            <TableHead>消息</TableHead>
                                            <TableHead className="w-[70px] text-right">操作</TableHead>
                                        </TableRow>
                                    </TableHeader>
                                    <TableBody>
                                        {filteredRows.map((row) => (
                                            <TableRow key={row.accountId}>
                                                <TableCell className="font-medium truncate max-w-[120px]">
                                                    <Tooltip>
                                                        <TooltipTrigger asChild>
                                                            <span className="truncate block">{row.siteName}</span>
                                                        </TooltipTrigger>
                                                        <TooltipContent>{row.siteName}</TooltipContent>
                                                    </Tooltip>
                                                </TableCell>
                                                <TableCell className="truncate max-w-[120px]">
                                                    <Tooltip>
                                                        <TooltipTrigger asChild>
                                                            <span className="truncate block">{row.accountName}</span>
                                                        </TooltipTrigger>
                                                        <TooltipContent>{row.accountName}</TooltipContent>
                                                    </Tooltip>
                                                </TableCell>
                                                <TableCell>
                                                    <Badge variant="outline" className={statusClassName(row.status)}>
                                                        {statusLabel(row.status)}
                                                    </Badge>
                                                </TableCell>
                                                <TableCell className="text-xs text-muted-foreground">
                                                    {formatDateTime(row.lastCheckinAt)}
                                                </TableCell>
                                                <TableCell className="text-xs text-muted-foreground truncate max-w-[200px]">
                                                    <Tooltip>
                                                        <TooltipTrigger asChild>
                                                            <span className="truncate block">{row.message || '—'}</span>
                                                        </TooltipTrigger>
                                                        <TooltipContent className="max-w-xs">{row.message || '—'}</TooltipContent>
                                                    </Tooltip>
                                                </TableCell>
                                                <TableCell className="text-right">
                                                    <Button
                                                        variant="ghost"
                                                        size="sm"
                                                        className="h-7 w-7 p-0 rounded-lg"
                                                        disabled={checkinLoadingIds.has(row.accountId)}
                                                        onClick={() => handleCheckinRow(row.accountId)}
                                                    >
                                                        {checkinLoadingIds.has(row.accountId) ? (
                                                            <RefreshCw className="size-3.5 animate-spin" />
                                                        ) : (
                                                            <CalendarCheck2 className="size-3.5" />
                                                        )}
                                                    </Button>
                                                </TableCell>
                                            </TableRow>
                                        ))}
                                    </TableBody>
                                </Table>
                            )}
                        </div>
                    </AccordionContent>
                </AccordionItem>
            </Accordion>
        </section>
    );
}
