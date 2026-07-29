'use client';

import { useState } from 'react';
import { History, ShieldAlert } from 'lucide-react';
import { useTranslations } from 'next-intl';
import {
    useRelayLogRepairExecute,
    useRelayLogRepairPreview,
} from '@/api/endpoints/log-analytics';
import { toast } from '@/components/common/Toast';
import { Badge } from '@/components/ui/badge';
import { Button } from '@/components/ui/button';
import {
    Dialog,
    DialogContent,
    DialogDescription,
    DialogHeader,
    DialogTitle,
    DialogTrigger,
} from '@/components/ui/dialog';
import { Switch } from '@/components/ui/switch';

function errorMessage(error: unknown) {
    return error instanceof Error ? error.message : String(error);
}

export function HistoricalRepairDialog({
    startTime,
    endTime,
}: {
    startTime?: number;
    endTime?: number;
}) {
    const t = useTranslations('log.analytics.repair');
    const preview = useRelayLogRepairPreview();
    const execute = useRelayLogRepairExecute();
    const [open, setOpen] = useState(false);
    const [confirmed, setConfirmed] = useState(false);
    const filter = { start_time: startTime, end_time: endTime };

    const changeOpen = (next: boolean) => {
        setOpen(next);
        if (!next) {
            setConfirmed(false);
            preview.reset();
            execute.reset();
        }
    };

    const runPreview = () => {
        preview.mutate(filter, {
            onError: (error) => toast.error(t('previewFailed'), { description: errorMessage(error) }),
        });
    };

    const runRepair = () => {
        execute.mutate(filter, {
            onSuccess: (result) => {
                setConfirmed(false);
                toast.success(t('complete'), { description: t('updated', { count: result.updated }) });
            },
            onError: (error) => toast.error(t('executeFailed'), { description: errorMessage(error) }),
        });
    };

    const result = execute.data ?? preview.data;

    return (
        <Dialog open={open} onOpenChange={changeOpen}>
            <DialogTrigger asChild>
                <Button type="button" variant="outline" size="sm">
                    <History />
                    {t('open')}
                </Button>
            </DialogTrigger>
            <DialogContent className="max-h-[calc(100dvh-2rem)] overflow-y-auto sm:max-w-2xl">
                <DialogHeader>
                    <DialogTitle>{t('title')}</DialogTitle>
                    <DialogDescription>{t('description')}</DialogDescription>
                </DialogHeader>
                <div className="space-y-4">
                    <div className="flex flex-wrap gap-2">
                        <Badge variant="outline">
                            {startTime ? new Date(startTime * 1000).toLocaleString() : t('unbounded')}
                        </Badge>
                        <Badge variant="outline">
                            {endTime ? new Date(endTime * 1000).toLocaleString() : t('unbounded')}
                        </Badge>
                    </div>

                    <Button type="button" variant="outline" onClick={runPreview} disabled={preview.isPending || execute.isPending}>
                        {preview.isPending ? t('previewing') : t('preview')}
                    </Button>

                    {result ? (
                        <>
                            <dl className="grid grid-cols-2 gap-3">
                                <div className="rounded-md border px-3 py-2">
                                    <dt className="text-xs text-muted-foreground">{t('matched')}</dt>
                                    <dd className="mt-1 text-lg font-semibold tabular-nums">{result.matched}</dd>
                                </div>
                                <div className="rounded-md border px-3 py-2">
                                    <dt className="text-xs text-muted-foreground">{t('excluded')}</dt>
                                    <dd className="mt-1 text-lg font-semibold tabular-nums">{result.excluded}</dd>
                                </div>
                            </dl>
                            <div className="flex flex-wrap gap-1.5">
                                {Object.entries(result.reasons ?? {}).map(([reason, count]) => (
                                    <Badge key={reason} variant="outline">{reason}: {count}</Badge>
                                ))}
                            </div>
                            <div className="max-h-56 overflow-auto rounded-md border">
                                {(result.samples ?? []).map((sample) => (
                                    <div key={sample.id} className="grid gap-1 border-b px-3 py-2 text-xs last:border-b-0 sm:grid-cols-[6rem_minmax(0,1fr)_auto]">
                                        <span>#{sample.id}</span>
                                        <span className="min-w-0 truncate">{sample.model} · {sample.channel}</span>
                                        <span className="tabular-nums">{sample.output_tokens} tokens</span>
                                    </div>
                                ))}
                            </div>
                        </>
                    ) : null}

                    {preview.data && preview.data.matched > 0 && !execute.data ? (
                        <div className="space-y-3 border-t pt-4">
                            <label className="flex min-h-11 items-center justify-between gap-3 text-sm">
                                <span className="inline-flex items-center gap-2">
                                    <ShieldAlert className="size-4 text-amber-600" />
                                    {t('confirm')}
                                </span>
                                <Switch
                                    checked={confirmed}
                                    onCheckedChange={setConfirmed}
                                    aria-label={t('confirm')}
                                />
                            </label>
                            <Button
                                type="button"
                                variant="destructive"
                                onClick={runRepair}
                                disabled={!confirmed || execute.isPending}
                            >
                                {execute.isPending ? t('executing') : t('execute')}
                            </Button>
                        </div>
                    ) : null}
                </div>
            </DialogContent>
        </Dialog>
    );
}
