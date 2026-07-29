import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { apiClient } from '../client';

export type ProtocolName =
    | 'openai_chat'
    | 'openai_responses'
    | 'anthropic'
    | 'gemini'
    | 'volcengine'
    | 'openai_embedding'
    | 'unknown';

export type ProtocolPolicy = 'inherit' | 'auto' | 'passthrough-only' | 'transform-allowed';
export type RoutingStrategy = 'balanced' | 'reliability' | 'lowest-cost' | 'lowest-latency' | 'manual';
export type RouteCandidateStatus =
    | 'active'
    | 'degraded'
    | 'stale'
    | 'unavailable'
    | 'disabled'
    | 'archived';

export type ProtocolFeature =
    | 'function_tools'
    | 'built_in_tools'
    | 'structured_output'
    | 'images'
    | 'audio'
    | 'files'
    | 'documents'
    | 'reasoning'
    | 'cache_control'
    | 'continuation'
    | 'responses_state'
    | 'provider_extensions'
    | 'anthropic_mcp'
    | 'anthropic_container'
    | 'anthropic_server_tools'
    | 'gemini_extensions'
    | 'websocket';

export type ModelAlias = {
    id: number;
    canonical_model_id: number;
    alias: string;
    normalized_alias: string;
    manual: boolean;
    created_at: string;
    updated_at: string;
};

export type RouteCandidate = {
    id: number;
    canonical_model_id: number;
    channel_id: number;
    upstream_model_name: string;
    site_id?: number | null;
    site_account_id?: number | null;
    site_group_key?: string;
    status: RouteCandidateStatus;
    priority: number;
    weight: number;
    protocol_policy: ProtocolPolicy;
    allow_lossy?: boolean | null;
    manual: boolean;
    last_seen_at: string;
    unavailable_since?: string | null;
    archived_at?: string | null;
    created_at: string;
    updated_at: string;
};

export type CanonicalModel = {
    id: number;
    name: string;
    normalized_name: string;
    routing_strategy: RoutingStrategy;
    protocol_policy: Exclude<ProtocolPolicy, 'inherit'>;
    allow_lossy: boolean;
    enabled: boolean;
    manual: boolean;
    created_at: string;
    updated_at: string;
    aliases: ModelAlias[];
    route_candidates: RouteCandidate[];
};

export type CanonicalModelUpdate = Pick<
    CanonicalModel,
    'id' | 'routing_strategy' | 'protocol_policy' | 'allow_lossy' | 'enabled'
>;

export type RouteCandidateUpdate = Pick<
    RouteCandidate,
    'id' | 'status' | 'priority' | 'weight' | 'protocol_policy' | 'allow_lossy'
>;

type CanonicalModelServer = Omit<CanonicalModel, 'aliases' | 'route_candidates'> & {
    aliases?: ModelAlias[] | null;
    route_candidates?: RouteCandidate[] | null;
};

export type CatalogSyncResult = {
    canonical_created: number;
    aliases_created: number;
    candidates_created: number;
    candidates_updated: number;
    marked_unavailable: number;
    archived: number;
    groups_created: number;
    group_items_created: number;
};

export type FeatureCapability = 'native' | 'transformed' | 'degraded' | 'unsupported';

export type ProtocolFeatureDecision = {
    feature: ProtocolFeature;
    capability: FeatureCapability;
    reason?: string;
};

export type ProtocolCapabilityDescriptor = {
    inbound_protocol: ProtocolName;
    outbound_protocol: ProtocolName;
    mode: 'passthrough' | 'transform';
    limited?: boolean;
    features: ProtocolFeatureDecision[];
};

export type RouteDecisionReason = {
    route_candidate_id: number;
    channel_id: number;
    upstream_model: string;
    status: RouteCandidateStatus;
    outbound_protocol: ProtocolName;
    protocol_mode?: 'passthrough' | 'transform';
    protocol_policy?: ProtocolPolicy;
    allow_lossy: boolean;
    compatibility?: 'exact' | 'lossy' | 'unsupported';
    capabilities?: ProtocolFeatureDecision[];
    warnings?: string[];
    included: boolean;
    reason: string;
    score: number;
};

export type RoutePreview = {
    requested_model: string;
    canonical_model: string;
    inbound_protocol: ProtocolName;
    features?: ProtocolFeature[];
    strategy: RoutingStrategy;
    decisions: RouteDecisionReason[];
};

export type RoutePreviewRequest = {
    model: string;
    inbound_protocol: ProtocolName;
    features?: ProtocolFeature[];
    websocket?: boolean;
};

function invalidateCatalog(queryClient: ReturnType<typeof useQueryClient>) {
    queryClient.invalidateQueries({ queryKey: ['models', 'catalog'] });
    queryClient.invalidateQueries({ queryKey: ['models', 'prices'] });
    queryClient.invalidateQueries({ queryKey: ['models', 'effective-price'] });
    queryClient.invalidateQueries({ queryKey: ['header-policies'] });
}

export function useModelCatalog() {
    return useQuery({
        queryKey: ['models', 'catalog'],
        queryFn: () => apiClient.get<CanonicalModelServer[]>('/api/v1/model/catalog'),
        select: (items): CanonicalModel[] =>
            (items ?? []).map((item) => ({
                ...item,
                aliases: item.aliases ?? [],
                route_candidates: item.route_candidates ?? [],
            })),
        refetchInterval: 30000,
    });
}

export function useSyncModelCatalog() {
    const queryClient = useQueryClient();
    return useMutation({
        mutationFn: () => apiClient.post<CatalogSyncResult>('/api/v1/model/catalog/sync', {}),
        onSuccess: () => invalidateCatalog(queryClient),
    });
}

export function useUpsertModelAlias() {
    const queryClient = useQueryClient();
    return useMutation({
        mutationFn: (payload: { canonical_model_id: number; alias: string }) =>
            apiClient.post<ModelAlias>('/api/v1/model/catalog/alias', payload),
        onSuccess: () => invalidateCatalog(queryClient),
    });
}

export function useDeleteModelAlias() {
    const queryClient = useQueryClient();
    return useMutation({
        mutationFn: (id: number) => apiClient.delete<null>(`/api/v1/model/catalog/alias/${id}`),
        onSuccess: () => invalidateCatalog(queryClient),
    });
}

export function useUpdateCanonicalModel() {
    const queryClient = useQueryClient();
    return useMutation({
        mutationFn: (payload: CanonicalModelUpdate) =>
            apiClient.post<CanonicalModelServer>('/api/v1/model/catalog/canonical', payload),
        onSuccess: () => invalidateCatalog(queryClient),
    });
}

export function useUpdateRouteCandidate() {
    const queryClient = useQueryClient();
    return useMutation({
        mutationFn: (payload: RouteCandidateUpdate) =>
            apiClient.post<RouteCandidate>('/api/v1/model/catalog/candidate', payload),
        onSuccess: () => invalidateCatalog(queryClient),
    });
}

export function useRoutePreview() {
    return useMutation({
        mutationFn: (payload: RoutePreviewRequest) =>
            apiClient.post<RoutePreview>('/api/v1/model/catalog/preview', payload),
    });
}

export function useProtocolCapabilities(enabled = true) {
    return useQuery({
        queryKey: ['models', 'protocol-capabilities'],
        queryFn: () =>
            apiClient.get<ProtocolCapabilityDescriptor[]>('/api/v1/model/protocol/capabilities'),
        select: (items) => items ?? [],
        enabled,
        staleTime: 5 * 60 * 1000,
    });
}
