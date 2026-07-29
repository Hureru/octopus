import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { apiClient } from '../client';

export type PriceQuoteSource =
    | 'manual_override'
    | 'site_exact'
    | 'site_wide'
    | 'site_stale'
    | 'global'
    | 'unknown';

export type PriceUnit = 'per_million_tokens' | 'per_request' | 'site_credit';
export type PriceQuoteStatus = 'valid' | 'rejected';

export type SiteModelPriceQuote = {
    id: number;
    identity_key: string;
    route_candidate_id?: number | null;
    site_id: number;
    site_account_id?: number | null;
    group_key: string;
    model_name: string;
    source: PriceQuoteSource;
    unit: PriceUnit;
    currency: string;
    input: number;
    output: number;
    cache_read: number;
    cache_write: number;
    per_request: number;
    group_multiplier: number;
    exchange_rate_to_usd: number;
    raw_payload?: string;
    observed_at: string;
    valid_until?: string | null;
    manual_override: boolean;
    status: PriceQuoteStatus;
    last_error?: string;
    created_at: string;
    updated_at: string;
};

export type ManualPriceQuoteInput = Pick<
    SiteModelPriceQuote,
    | 'site_id'
    | 'group_key'
    | 'model_name'
    | 'unit'
    | 'currency'
    | 'input'
    | 'output'
    | 'cache_read'
    | 'cache_write'
    | 'per_request'
    | 'group_multiplier'
    | 'exchange_rate_to_usd'
> & {
    route_candidate_id?: number | null;
    site_account_id?: number | null;
};

export type CurrencyRate = {
    currency: string;
    rate_to_usd: number;
    updated_at: string;
};

export type EffectivePrice = {
    quote_id?: number;
    route_candidate_id?: number;
    source: PriceQuoteSource;
    unit: PriceUnit;
    currency: string;
    input: number;
    output: number;
    cache_read: number;
    cache_write: number;
    per_request: number;
    group_multiplier: number;
    exchange_rate_to_usd: number;
    observed_at?: string;
    stale: boolean;
    convertible: boolean;
    match_reason?: string;
};

function invalidatePricing(queryClient: ReturnType<typeof useQueryClient>) {
    queryClient.invalidateQueries({ queryKey: ['models', 'prices'] });
    queryClient.invalidateQueries({ queryKey: ['models', 'effective-price'] });
}

export function useSiteModelPrices(
    filters: { canonicalModelId?: number; routeCandidateId?: number },
    enabled = true,
) {
    return useQuery({
        queryKey: ['models', 'prices', filters.canonicalModelId ?? 0, filters.routeCandidateId ?? 0],
        queryFn: () =>
            apiClient.get<SiteModelPriceQuote[]>('/api/v1/model/prices', {
                ...(filters.canonicalModelId ? { canonical_model_id: filters.canonicalModelId } : {}),
                ...(filters.routeCandidateId ? { route_candidate_id: filters.routeCandidateId } : {}),
            }),
        select: (items) => items ?? [],
        enabled,
    });
}

export function useEffectivePrice(candidateId: number | null, model: string, enabled = true) {
    return useQuery({
        queryKey: ['models', 'effective-price', candidateId ?? 0, model],
        queryFn: () =>
            apiClient.get<EffectivePrice>('/api/v1/model/effective-price', {
                ...(candidateId ? { route_candidate_id: candidateId } : {}),
                ...(model ? { model } : {}),
            }),
        enabled: enabled && (!!candidateId || !!model),
    });
}

export function useCurrencyRates() {
    return useQuery({
        queryKey: ['models', 'currency-rates'],
        queryFn: () => apiClient.get<CurrencyRate[]>('/api/v1/model/currency-rates'),
        select: (items) => items ?? [],
    });
}

export function useUpsertCurrencyRate() {
    const queryClient = useQueryClient();
    return useMutation({
        mutationFn: (payload: Pick<CurrencyRate, 'currency' | 'rate_to_usd'>) =>
            apiClient.post<CurrencyRate>('/api/v1/model/currency-rate', payload),
        onSuccess: () => {
            queryClient.invalidateQueries({ queryKey: ['models', 'currency-rates'] });
            invalidatePricing(queryClient);
        },
    });
}

export function useUpsertSiteModelPrice() {
    const queryClient = useQueryClient();
    return useMutation({
        mutationFn: (payload: ManualPriceQuoteInput) =>
            apiClient.post<SiteModelPriceQuote>('/api/v1/model/price-quote', payload),
        onSuccess: () => invalidatePricing(queryClient),
    });
}

export function useDeleteSiteModelPrice() {
    const queryClient = useQueryClient();
    return useMutation({
        mutationFn: (id: number) => apiClient.delete<null>(`/api/v1/model/price-quote/${id}`),
        onSuccess: () => invalidatePricing(queryClient),
    });
}
