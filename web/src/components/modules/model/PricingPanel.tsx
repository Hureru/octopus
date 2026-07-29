'use client';

import { useState } from 'react';
import { CircleDollarSign, Plus, Trash2 } from 'lucide-react';
import { useTranslations } from 'next-intl';
import type { RouteCandidate } from '@/api/endpoints/model-catalog';
import {
    type ManualPriceQuoteInput,
    type PriceQuoteSource,
    type PriceUnit,
    useCurrencyRates,
    useDeleteSiteModelPrice,
    useEffectivePrice,
    useSiteModelPrices,
    useUpsertCurrencyRate,
    useUpsertSiteModelPrice,
} from '@/api/endpoints/model-pricing';
import { toast } from '@/components/common/Toast';
import { Badge } from '@/components/ui/badge';
import { Button } from '@/components/ui/button';
import { Input } from '@/components/ui/input';
import {
    Select,
    SelectContent,
    SelectItem,
    SelectTrigger,
    SelectValue,
} from '@/components/ui/select';
import { cn } from '@/lib/utils';

const PRICE_CHAIN: PriceQuoteSource[] = [
    'manual_override',
    'site_exact',
    'site_wide',
    'site_stale',
    'global',
    'unknown',
];

type PriceForm = {
    unit: PriceUnit;
    currency: string;
    input: string;
    output: string;
    cacheRead: string;
    cacheWrite: string;
    perRequest: string;
    multiplier: string;
    exchangeRate: string;
};

const EMPTY_PRICE_FORM: PriceForm = {
    unit: 'per_million_tokens',
    currency: 'USD',
    input: '0',
    output: '0',
    cacheRead: '0',
    cacheWrite: '0',
    perRequest: '0',
    multiplier: '1',
    exchangeRate: '0',
};

const PRICE_NUMBER_FORMATTER = new Intl.NumberFormat(undefined, {
    maximumSignificantDigits: 12,
});

function formatPriceNumber(value: number) {
    return Number.isFinite(value) ? PRICE_NUMBER_FORMATTER.format(value) : '-';
}

function errorMessage(error: unknown) {
    return error instanceof Error ? error.message : String(error);
}

function numberValue(value: string) {
    const parsed = Number(value);
    return Number.isFinite(parsed) && parsed >= 0 ? parsed : 0;
}

export function PricingPanel({ candidate }: { candidate: RouteCandidate }) {
    const t = useTranslations('model.pricing');
    const effective = useEffectivePrice(candidate.id, candidate.upstream_model_name);
    const quotes = useSiteModelPrices({ routeCandidateId: candidate.id });
    const rates = useCurrencyRates();
    const upsertQuote = useUpsertSiteModelPrice();
    const deleteQuote = useDeleteSiteModelPrice();
    const upsertRate = useUpsertCurrencyRate();
    const [form, setForm] = useState<PriceForm>(EMPTY_PRICE_FORM);
    const [rateCurrency, setRateCurrency] = useState('');
    const [rateValue, setRateValue] = useState('');

    const saveQuote = () => {
        if (!candidate.site_id) return;
        const payload: ManualPriceQuoteInput = {
            route_candidate_id: candidate.id,
            site_id: candidate.site_id,
            site_account_id: candidate.site_account_id ?? null,
            group_key: candidate.site_group_key ?? '',
            model_name: candidate.upstream_model_name,
            unit: form.unit,
            currency: form.currency.trim().toUpperCase(),
            input: numberValue(form.input),
            output: numberValue(form.output),
            cache_read: numberValue(form.cacheRead),
            cache_write: numberValue(form.cacheWrite),
            per_request: numberValue(form.perRequest),
            group_multiplier: numberValue(form.multiplier) || 1,
            exchange_rate_to_usd: numberValue(form.exchangeRate),
        };
        upsertQuote.mutate(payload, {
            onSuccess: () => toast.success(t('quoteSaved')),
            onError: (error) => toast.error(t('quoteSaveFailed'), { description: errorMessage(error) }),
        });
    };

    const saveRate = () => {
        const currency = rateCurrency.trim().toUpperCase();
        const rate = Number(rateValue);
        if (!currency || !Number.isFinite(rate) || rate <= 0) {
            toast.error(t('invalidRate'));
            return;
        }
        upsertRate.mutate({ currency, rate_to_usd: rate }, {
            onSuccess: () => {
                setRateCurrency('');
                setRateValue('');
                toast.success(t('rateSaved'));
            },
            onError: (error) => toast.error(t('rateSaveFailed'), { description: errorMessage(error) }),
        });
    };

    return (
        <section className="space-y-4 border-t pt-4">
            <div className="flex items-center gap-2">
                <CircleDollarSign className="size-4 text-muted-foreground" />
                <h3 className="text-sm font-semibold">{t('title')}</h3>
            </div>

            {effective.data ? (
                <div className="space-y-3">
                    <div className="flex flex-wrap gap-1">
                        {PRICE_CHAIN.map((source) => (
                            <Badge
                                key={source}
                                variant={source === effective.data?.source ? 'default' : 'outline'}
                                className={cn(source !== effective.data?.source && 'text-muted-foreground')}
                            >
                                {t(`source.${source}`)}
                            </Badge>
                        ))}
                    </div>
                    <dl className="grid gap-2 text-sm sm:grid-cols-3">
                        <PriceMetric label={t('input')} value={effective.data.input} currency={effective.data.currency} />
                        <PriceMetric label={t('output')} value={effective.data.output} currency={effective.data.currency} />
                        <PriceMetric label={t('cacheRead')} value={effective.data.cache_read} currency={effective.data.currency} />
                        <PriceMetric label={t('cacheWrite')} value={effective.data.cache_write} currency={effective.data.currency} />
                        <PriceMetric label={t('perRequest')} value={effective.data.per_request} currency={effective.data.currency} />
                        <div className="rounded-md border px-3 py-2">
                            <dt className="text-xs text-muted-foreground">{t('exchangeRate')}</dt>
                            <dd className="mt-1 font-medium tabular-nums">
                                {effective.data.convertible
                                    ? formatPriceNumber(effective.data.exchange_rate_to_usd)
                                    : t('unknown')}
                            </dd>
                        </div>
                    </dl>
                    <div className="flex flex-wrap gap-2 text-xs text-muted-foreground">
                        <span>{t(`unit.${effective.data.unit}`)}</span>
                        <span>{t('multiplier')}: {formatPriceNumber(effective.data.group_multiplier)}</span>
                        {effective.data.stale ? <Badge variant="destructive">{t('stale')}</Badge> : null}
                        {!effective.data.convertible ? <Badge variant="outline">{t('notConvertible')}</Badge> : null}
                        {effective.data.match_reason ? <span className="break-words">{effective.data.match_reason}</span> : null}
                    </div>
                </div>
            ) : effective.error ? (
                <div role="alert" className="rounded-md border border-destructive/30 bg-destructive/10 p-3 text-sm text-destructive">
                    {errorMessage(effective.error)}
                </div>
            ) : (
                <p className="text-sm text-muted-foreground">{effective.isLoading ? t('loading') : t('unknown')}</p>
            )}

            <div className="space-y-2 border-t pt-4">
                <div className="flex items-center justify-between">
                    <h4 className="text-sm font-medium">{t('quotes')}</h4>
                    <Badge variant="outline">{quotes.data?.length ?? 0}</Badge>
                </div>
                {quotes.error ? (
                    <div role="alert" className="rounded-md border border-destructive/30 bg-destructive/10 p-3 text-sm text-destructive">
                        {errorMessage(quotes.error)}
                    </div>
                ) : (quotes.data ?? []).length > 0 ? (
                    <div className="divide-y rounded-md border">
                        {(quotes.data ?? []).map((quote) => (
                            <div key={quote.id} className="flex min-w-0 items-start gap-3 px-3 py-2 text-sm">
                                <div className="min-w-0 flex-1">
                                    <div className="flex flex-wrap items-center gap-1.5">
                                        <Badge variant={quote.manual_override ? 'default' : 'outline'}>
                                            {t(`source.${quote.source}`)}
                                        </Badge>
                                        <span>{quote.currency}</span>
                                        <span>{t(`unit.${quote.unit}`)}</span>
                                    </div>
                                    <p className="mt-1 break-words text-xs text-muted-foreground">
                                        {t('input')} {formatPriceNumber(quote.input)} · {t('output')} {formatPriceNumber(quote.output)} · {t('multiplier')} {formatPriceNumber(quote.group_multiplier)}
                                    </p>
                                    {quote.last_error ? (
                                        <p className="mt-1 break-words text-xs text-destructive">{quote.last_error}</p>
                                    ) : null}
                                </div>
                                {quote.manual_override ? (
                                    <Button
                                        type="button"
                                        variant="ghost"
                                        size="icon-sm"
                                        aria-label={t('deleteQuote')}
                                        onClick={() =>
                                            deleteQuote.mutate(quote.id, {
                                                onError: (error) =>
                                                    toast.error(t('quoteDeleteFailed'), { description: errorMessage(error) }),
                                            })
                                        }
                                    >
                                        <Trash2 />
                                    </Button>
                                ) : null}
                            </div>
                        ))}
                    </div>
                ) : (
                    <p className="text-sm text-muted-foreground">{t('noQuotes')}</p>
                )}
            </div>

            {candidate.site_id ? (
                <div className="space-y-3 border-t pt-4">
                    <h4 className="text-sm font-medium">{t('manualOverride')}</h4>
                    <div className="grid gap-3 sm:grid-cols-3">
                        <Select
                            value={form.unit}
                            onValueChange={(value) => setForm({ ...form, unit: value as PriceUnit })}
                        >
                            <SelectTrigger className="w-full" aria-label={t('unitLabel')}><SelectValue /></SelectTrigger>
                            <SelectContent>
                                <SelectItem value="per_million_tokens">{t('unit.per_million_tokens')}</SelectItem>
                                <SelectItem value="per_request">{t('unit.per_request')}</SelectItem>
                                <SelectItem value="site_credit">{t('unit.site_credit')}</SelectItem>
                            </SelectContent>
                        </Select>
                        <LabeledInput
                            label={t('currency')}
                            value={form.currency}
                            onChange={(value) => setForm({ ...form, currency: value })}
                        />
                        <LabeledInput
                            label={t('multiplier')}
                            type="number"
                            value={form.multiplier}
                            onChange={(value) => setForm({ ...form, multiplier: value })}
                        />
                        <LabeledInput label={t('input')} type="number" value={form.input} onChange={(value) => setForm({ ...form, input: value })} />
                        <LabeledInput label={t('output')} type="number" value={form.output} onChange={(value) => setForm({ ...form, output: value })} />
                        <LabeledInput label={t('cacheRead')} type="number" value={form.cacheRead} onChange={(value) => setForm({ ...form, cacheRead: value })} />
                        <LabeledInput label={t('cacheWrite')} type="number" value={form.cacheWrite} onChange={(value) => setForm({ ...form, cacheWrite: value })} />
                        <LabeledInput label={t('perRequest')} type="number" value={form.perRequest} onChange={(value) => setForm({ ...form, perRequest: value })} />
                        <LabeledInput label={t('exchangeRate')} type="number" value={form.exchangeRate} onChange={(value) => setForm({ ...form, exchangeRate: value })} />
                    </div>
                    <Button type="button" onClick={saveQuote} disabled={upsertQuote.isPending}>
                        <Plus />
                        {upsertQuote.isPending ? t('saving') : t('saveOverride')}
                    </Button>
                </div>
            ) : null}

            <div className="space-y-3 border-t pt-4">
                    <div className="flex items-center justify-between">
                    <h4 className="text-sm font-medium">{t('currencyRates')}</h4>
                    {rates.error ? (
                        <span role="alert" className="text-xs text-destructive">{errorMessage(rates.error)}</span>
                    ) : (
                    <div className="flex flex-wrap gap-1">
                        {(rates.data ?? []).map((rate) => (
                            <Badge key={rate.currency} variant="outline">
                                {rate.currency} {formatPriceNumber(rate.rate_to_usd)}
                            </Badge>
                        ))}
                    </div>
                    )}
                </div>
                <div className="grid gap-2 sm:grid-cols-[8rem_10rem_auto]">
                    <Input
                        aria-label={t('currency')}
                        value={rateCurrency}
                        onChange={(event) => setRateCurrency(event.target.value)}
                        placeholder="EUR"
                    />
                    <Input
                        aria-label={t('exchangeRate')}
                        type="number"
                        min="0"
                        step="any"
                        value={rateValue}
                        onChange={(event) => setRateValue(event.target.value)}
                        placeholder="1.08"
                    />
                    <Button type="button" variant="outline" onClick={saveRate} disabled={upsertRate.isPending}>
                        {t('saveRate')}
                    </Button>
                </div>
            </div>
        </section>
    );
}

function PriceMetric({ label, value, currency }: { label: string; value: number; currency: string }) {
    return (
        <div className="rounded-md border px-3 py-2">
            <dt className="text-xs text-muted-foreground">{label}</dt>
            <dd className="mt-1 font-medium tabular-nums">{formatPriceNumber(value)} {currency}</dd>
        </div>
    );
}

function LabeledInput({
    label,
    value,
    onChange,
    type = 'text',
}: {
    label: string;
    value: string;
    onChange: (value: string) => void;
    type?: 'text' | 'number';
}) {
    return (
        <label className="grid gap-1 text-xs text-muted-foreground">
            {label}
            <Input
                type={type}
                min={type === 'number' ? '0' : undefined}
                step={type === 'number' ? 'any' : undefined}
                value={value}
                onChange={(event) => onChange(event.target.value)}
            />
        </label>
    );
}
