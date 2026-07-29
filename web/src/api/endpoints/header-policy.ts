import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import type { CustomHeader } from './channel';
import { apiClient } from '../client';

export type HeaderPolicyScope =
    | 'global'
    | 'site'
    | 'site_account'
    | 'channel'
    | 'canonical_model'
    | 'route_candidate';

export type HeaderPolicy = {
    id: number;
    name: string;
    version: number;
    scope: HeaderPolicyScope;
    scope_id: number;
    enabled: boolean;
    forward_client_headers?: boolean | null;
    user_agent?: string | null;
    set_headers: CustomHeader[];
    unset_headers: string[];
    allowed_client_headers?: string[] | null;
    created_at: string;
    updated_at: string;
};

export type HeaderPolicyUpsertInput = Omit<HeaderPolicy, 'created_at' | 'updated_at' | 'version'>;

type HeaderPolicyServer = Omit<HeaderPolicy, 'set_headers' | 'unset_headers'> & {
    set_headers?: CustomHeader[] | null;
    unset_headers?: string[] | null;
};

export type UserAgentProfile = {
    id: number;
    name: string;
    value: string;
    built_in: boolean;
    created_at: string;
    updated_at: string;
};

export type HeaderPolicyTrace = {
    scope: HeaderPolicyScope;
    scope_id: number;
    policy_id: number;
    policy_name: string;
    policy_version: number;
    applied_keys?: string[];
    unset_keys?: string[];
};

export type ResolvedHeaderPolicy = {
    forward_client_headers: boolean;
    user_agent?: string;
    set_headers: CustomHeader[];
    unset_headers: string[];
    allowed_client_headers: string[];
    trace: HeaderPolicyTrace[];
};

export type HeaderPolicyRegistry = {
    default_allowed_client_headers: string[];
    protected_headers: string[];
    protected_prefixes: string[];
};

export type HeaderPolicyPreviewParams = {
    channelId?: number;
    canonicalModelId?: number;
    routeCandidateId?: number;
};

function invalidatePolicies(queryClient: ReturnType<typeof useQueryClient>) {
    queryClient.invalidateQueries({ queryKey: ['header-policies'] });
}

export function useHeaderPolicies() {
    return useQuery({
        queryKey: ['header-policies', 'list'],
        queryFn: () => apiClient.get<HeaderPolicyServer[]>('/api/v1/header-policy/list'),
        select: (items): HeaderPolicy[] =>
            (items ?? []).map((item) => ({
                ...item,
                set_headers: item.set_headers ?? [],
                unset_headers: item.unset_headers ?? [],
                allowed_client_headers: item.allowed_client_headers ?? null,
            })),
    });
}

export function useHeaderPolicyRegistry() {
    return useQuery({
        queryKey: ['header-policies', 'registry'],
        queryFn: () => apiClient.get<HeaderPolicyRegistry>('/api/v1/header-policy/registry'),
        staleTime: 10 * 60 * 1000,
    });
}

export function useUpsertHeaderPolicy() {
    const queryClient = useQueryClient();
    return useMutation({
        mutationFn: (payload: HeaderPolicyUpsertInput) =>
            apiClient.post<HeaderPolicyServer>('/api/v1/header-policy/upsert', payload),
        onSuccess: () => invalidatePolicies(queryClient),
    });
}

export function useDeleteHeaderPolicy() {
    const queryClient = useQueryClient();
    return useMutation({
        mutationFn: (id: number) => apiClient.delete<null>(`/api/v1/header-policy/${id}`),
        onSuccess: () => invalidatePolicies(queryClient),
    });
}

export function useUserAgentProfiles() {
    return useQuery({
        queryKey: ['header-policies', 'user-agents'],
        queryFn: () => apiClient.get<UserAgentProfile[]>('/api/v1/header-policy/user-agents'),
        select: (items) => items ?? [],
    });
}

export function useUpsertUserAgentProfile() {
    const queryClient = useQueryClient();
    return useMutation({
        mutationFn: (payload: Pick<UserAgentProfile, 'id' | 'name' | 'value'>) =>
            apiClient.post<UserAgentProfile>('/api/v1/header-policy/user-agent', payload),
        onSuccess: () => queryClient.invalidateQueries({ queryKey: ['header-policies', 'user-agents'] }),
    });
}

export function useHeaderPolicyPreview(params: HeaderPolicyPreviewParams, enabled = true) {
    return useQuery({
        queryKey: [
            'header-policies',
            'preview',
            params.channelId ?? 0,
            params.canonicalModelId ?? 0,
            params.routeCandidateId ?? 0,
        ],
        queryFn: () =>
            apiClient.get<ResolvedHeaderPolicy>('/api/v1/header-policy/preview', {
                ...(params.channelId ? { channel_id: params.channelId } : {}),
                ...(params.canonicalModelId ? { canonical_model_id: params.canonicalModelId } : {}),
                ...(params.routeCandidateId ? { route_candidate_id: params.routeCandidateId } : {}),
            }),
        enabled,
    });
}
