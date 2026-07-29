'use client';

import { useMemo, useState } from 'react';
import { Check, FlaskConical, Network, X } from 'lucide-react';
import { useTranslations } from 'next-intl';
import {
    type ProtocolFeature,
    type ProtocolName,
    type RoutePreviewRequest,
    useProtocolCapabilities,
    useRoutePreview,
} from '@/api/endpoints/model-catalog';
import { toast } from '@/components/common/Toast';
import { Badge } from '@/components/ui/badge';
import { Button } from '@/components/ui/button';
import {
    Dialog,
    DialogContent,
    DialogDescription,
    DialogHeader,
    DialogTitle,
} from '@/components/ui/dialog';
import {
    Select,
    SelectContent,
    SelectItem,
    SelectTrigger,
    SelectValue,
} from '@/components/ui/select';
import { Switch } from '@/components/ui/switch';
import { cn } from '@/lib/utils';
import { INBOUND_PROTOCOLS, PROTOCOL_FEATURES } from './catalog-options';

function errorMessage(error: unknown) {
    return error instanceof Error ? error.message : String(error);
}

export function RouteTools({ model }: { model: string }) {
    const t = useTranslations('model.catalog');
    const [previewOpen, setPreviewOpen] = useState(false);
    const [matrixOpen, setMatrixOpen] = useState(false);

    return (
        <div className="flex flex-wrap gap-2">
            <Button type="button" variant="outline" size="sm" onClick={() => setPreviewOpen(true)}>
                <Network />
                {t('routePreview')}
            </Button>
            <Button type="button" variant="outline" size="sm" onClick={() => setMatrixOpen(true)}>
                <FlaskConical />
                {t('capabilityMatrix')}
            </Button>
            <RoutePreviewDialog model={model} open={previewOpen} onOpenChange={setPreviewOpen} />
            <CapabilityMatrixDialog open={matrixOpen} onOpenChange={setMatrixOpen} />
        </div>
    );
}

function RoutePreviewDialog({
    model,
    open,
    onOpenChange,
}: {
    model: string;
    open: boolean;
    onOpenChange: (open: boolean) => void;
}) {
    const t = useTranslations('model.catalog');
    const preview = useRoutePreview();
    const [request, setRequest] = useState<RoutePreviewRequest>({
        model,
        inbound_protocol: 'openai_chat',
        features: [],
        websocket: false,
    });

    const toggleFeature = (feature: ProtocolFeature) => {
        const current = new Set(request.features ?? []);
        if (current.has(feature)) current.delete(feature);
        else current.add(feature);
        setRequest({ ...request, features: [...current] });
    };

    const runPreview = () => {
        preview.mutate({ ...request, model: request.model.trim() || model }, {
            onError: (error) => toast.error(t('previewFailed'), { description: errorMessage(error) }),
        });
    };

    return (
        <Dialog open={open} onOpenChange={onOpenChange}>
            <DialogContent className="max-h-[calc(100dvh-2rem)] overflow-hidden sm:max-w-4xl">
                <DialogHeader>
                    <DialogTitle>{t('routePreview')}</DialogTitle>
                    <DialogDescription className="sr-only">{t('routePreview')}</DialogDescription>
                </DialogHeader>
                <div className="grid min-h-0 gap-4 overflow-y-auto md:grid-cols-[18rem_minmax(0,1fr)]">
                    <div className="space-y-4">
                        <label className="grid gap-1.5 text-sm font-medium">
                            {t('requestedModel')}
                            <input
                                value={request.model}
                                onChange={(event) => setRequest({ ...request, model: event.target.value })}
                                className="h-9 rounded-md border bg-background px-3 text-sm outline-none focus-visible:ring-2 focus-visible:ring-ring"
                            />
                        </label>
                        <label className="grid gap-1.5 text-sm font-medium">
                            {t('inboundProtocol')}
                            <Select
                                value={request.inbound_protocol}
                                onValueChange={(value) =>
                                    setRequest({ ...request, inbound_protocol: value as ProtocolName })
                                }
                            >
                                <SelectTrigger className="w-full">
                                    <SelectValue />
                                </SelectTrigger>
                                <SelectContent>
                                    {INBOUND_PROTOCOLS.map((protocol) => (
                                        <SelectItem key={protocol} value={protocol}>
                                            {t(`protocol.${protocol}`)}
                                        </SelectItem>
                                    ))}
                                </SelectContent>
                            </Select>
                        </label>
                        <label className="flex min-h-11 items-center justify-between gap-3 text-sm font-medium">
                            WebSocket
                            <Switch
                                checked={request.websocket}
                                onCheckedChange={(checked) => setRequest({ ...request, websocket: checked })}
                            />
                        </label>
                        <div className="flex flex-wrap gap-1.5">
                            {PROTOCOL_FEATURES.map((feature) => {
                                const selected = request.features?.includes(feature);
                                return (
                                    <button
                                        key={feature}
                                        type="button"
                                        aria-pressed={selected}
                                        onClick={() => toggleFeature(feature)}
                                        className={cn(
                                            'min-h-8 rounded-md border px-2 text-xs transition-colors',
                                            selected ? 'border-primary bg-primary text-primary-foreground' : 'hover:bg-muted',
                                        )}
                                    >
                                        {t(`feature.${feature}`)}
                                    </button>
                                );
                            })}
                        </div>
                        <Button type="button" className="w-full" onClick={runPreview} disabled={preview.isPending}>
                            {preview.isPending ? t('runningPreview') : t('runPreview')}
                        </Button>
                    </div>
                    <div className="min-w-0 space-y-3">
                        {preview.data ? (
                            <>
                                <div className="flex flex-wrap items-center gap-2">
                                    <Badge variant="outline">{preview.data.canonical_model}</Badge>
                                    <Badge variant="secondary">{t(`strategy.${preview.data.strategy}`)}</Badge>
                                </div>
                                <div className="divide-y rounded-md border">
                                    {preview.data.decisions.map((decision) => (
                                        <div key={`${decision.channel_id}:${decision.upstream_model}`} className="p-3">
                                            <div className="flex min-w-0 items-center gap-2">
                                                {decision.included ? (
                                                    <Check className="size-4 shrink-0 text-emerald-600" />
                                                ) : (
                                                    <X className="size-4 shrink-0 text-destructive" />
                                                )}
                                                <span className="min-w-0 flex-1 truncate font-medium">
                                                    {decision.upstream_model}
                                                </span>
                                                <Badge variant="outline">#{decision.channel_id}</Badge>
                                            </div>
                                            <div className="mt-2 flex flex-wrap gap-1.5 text-xs text-muted-foreground">
                                                <span>{t(`protocol.${decision.outbound_protocol}`)}</span>
                                                <span>{decision.protocol_mode ?? '-'}</span>
                                                <span>{decision.compatibility ?? '-'}</span>
                                                <span>{decision.reason}</span>
                                            </div>
                                            {(decision.warnings ?? []).map((warning) => (
                                                <p key={warning} className="mt-1 break-words text-xs text-amber-700 dark:text-amber-300">
                                                    {warning}
                                                </p>
                                            ))}
                                        </div>
                                    ))}
                                </div>
                            </>
                        ) : (
                            <div className="grid min-h-48 place-items-center text-sm text-muted-foreground">
                                {t('noPreview')}
                            </div>
                        )}
                    </div>
                </div>
            </DialogContent>
        </Dialog>
    );
}

function CapabilityMatrixDialog({
    open,
    onOpenChange,
}: {
    open: boolean;
    onOpenChange: (open: boolean) => void;
}) {
    const t = useTranslations('model.catalog');
    const capabilities = useProtocolCapabilities(open);
    const [inbound, setInbound] = useState<ProtocolName>('openai_chat');
    const [outbound, setOutbound] = useState<ProtocolName>('openai_chat');
    const outboundOptions = useMemo(
        () => [...new Set((capabilities.data ?? []).filter((item) => item.inbound_protocol === inbound).map((item) => item.outbound_protocol))],
        [capabilities.data, inbound],
    );
    const entry = (capabilities.data ?? []).find(
        (item) => item.inbound_protocol === inbound && item.outbound_protocol === outbound,
    );

    return (
        <Dialog open={open} onOpenChange={onOpenChange}>
            <DialogContent className="max-h-[calc(100dvh-2rem)] overflow-hidden sm:max-w-2xl">
                <DialogHeader>
                    <DialogTitle>{t('capabilityMatrix')}</DialogTitle>
                    <DialogDescription className="sr-only">{t('capabilityMatrix')}</DialogDescription>
                </DialogHeader>
                <div className="flex min-h-0 flex-col gap-4">
                    <div className="grid gap-3 sm:grid-cols-2">
                        <Select
                            value={inbound}
                            onValueChange={(value) => {
                                const next = value as ProtocolName;
                                setInbound(next);
                                const first = capabilities.data?.find((item) => item.inbound_protocol === next);
                                if (first) setOutbound(first.outbound_protocol);
                            }}
                        >
                            <SelectTrigger className="w-full" aria-label={t('inboundProtocol')}>
                                <SelectValue />
                            </SelectTrigger>
                            <SelectContent>
                                {INBOUND_PROTOCOLS.map((protocol) => (
                                    <SelectItem key={protocol} value={protocol}>{t(`protocol.${protocol}`)}</SelectItem>
                                ))}
                            </SelectContent>
                        </Select>
                        <Select value={outbound} onValueChange={(value) => setOutbound(value as ProtocolName)}>
                            <SelectTrigger className="w-full" aria-label={t('outboundProtocol')}>
                                <SelectValue />
                            </SelectTrigger>
                            <SelectContent>
                                {outboundOptions.map((protocol) => (
                                    <SelectItem key={protocol} value={protocol}>{t(`protocol.${protocol}`)}</SelectItem>
                                ))}
                            </SelectContent>
                        </Select>
                    </div>
                    {entry ? (
                        <div className="min-h-0 overflow-y-auto rounded-md border">
                            <div className="flex items-center gap-2 border-b px-3 py-2">
                                <Badge>{entry.mode}</Badge>
                                {entry.limited ? <Badge variant="destructive">{t('limited')}</Badge> : null}
                            </div>
                            <div className="divide-y">
                                {entry.features.map((feature) => (
                                    <div key={feature.feature} className="grid gap-1 px-3 py-2 sm:grid-cols-[12rem_7rem_minmax(0,1fr)]">
                                        <span className="text-sm font-medium">{t(`feature.${feature.feature}`)}</span>
                                        <Badge variant="outline">{feature.capability}</Badge>
                                        <span className="break-words text-xs text-muted-foreground">{feature.reason}</span>
                                    </div>
                                ))}
                            </div>
                        </div>
                    ) : (
                        <div className="grid min-h-40 place-items-center text-sm text-muted-foreground">
                            {capabilities.isLoading ? t('loading') : t('unsupportedRoute')}
                        </div>
                    )}
                </div>
            </DialogContent>
        </Dialog>
    );
}
