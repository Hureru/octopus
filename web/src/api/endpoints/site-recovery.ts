import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { apiClient } from '../client';
import type { ProxyMode } from './proxy-pool';

export type SiteRecoveryOperation = 'sync' | 'checkin';
export type SiteProxyPreferenceStatus = 'healthy' | 'cooling' | 'stale' | 'disabled';
export type VerificationSessionStatus = 'pending' | 'completed' | 'expired' | 'revoked';
export type VerificationTaskStatus = 'pending' | 'claimed' | 'completed' | 'expired' | 'canceled';
export type VerificationRetryStatus =
    | 'none'
    | 'pending'
    | 'running'
    | 'succeeded'
    | 'failed'
    | 'canceled';

export type ClashController = {
    id: number;
    name: string;
    api_url: string;
    proxy_url: string;
    group_name: string;
    enabled: boolean;
    created_at: string;
    updated_at: string;
};

export type ClashControllerInput = {
    id?: number;
    name: string;
    api_url: string;
    proxy_url: string;
    group_name: string;
    secret?: string;
    enabled: boolean;
};

export type ClashGroupState = {
    now: string;
    all: string[];
};

export type SiteProxyPreference = {
    id: number;
    identity_key: string;
    site_id: number;
    site_account_id: number;
    proxy_mode: Exclude<ProxyMode, 'inherit'>;
    proxy_config_id: number;
    clash_controller_id: number;
    clash_node?: string;
    status: SiteProxyPreferenceStatus;
    consecutive_failures: number;
    success_count: number;
    failure_count: number;
    average_latency_ms: number;
    cooldown_until?: string | null;
    last_success_at?: string | null;
    last_failure_at?: string | null;
    expires_at?: string | null;
    manual: boolean;
    created_at: string;
    updated_at: string;
};

export type SiteOperationAttempt = {
    id: number;
    site_id: number;
    site_account_id: number;
    operation: SiteRecoveryOperation;
    attempt_number: number;
    proxy_mode: Exclude<ProxyMode, 'inherit'>;
    proxy_config_id?: number | null;
    clash_controller_id?: number | null;
    clash_node?: string;
    started_at: string;
    duration_ms: number;
    success: boolean;
    failure_class?: string;
    message?: string;
    operation_id?: string;
    path_label?: string;
    stop_reason?: string;
};

export type VerificationSession = {
    id: number;
    site_id: number;
    site_account_id: number;
    proxy_config_id?: number | null;
    clash_node?: string;
    user_agent?: string;
    status: VerificationSessionStatus;
    expires_at: string;
    completed_at?: string | null;
    source?: string;
    created_at: string;
};

export type VerificationTask = {
    id: number;
    session_id: number;
    pairing_id?: number | null;
    status: VerificationTaskStatus;
    target_url: string;
    target_host: string;
    proxy_config_id?: number | null;
    clash_node?: string;
    user_agent?: string;
    expires_at: string;
    claimed_at?: string | null;
    completed_at?: string | null;
    operation?: SiteRecoveryOperation | '';
    retry_status: VerificationRetryStatus;
    retry_message?: string;
    retry_started_at?: string | null;
    retry_completed_at?: string | null;
    created_at: string;
};

export type VerificationBridgePairing = {
    id: number;
    name: string;
    expires_at: string;
    last_seen_at?: string | null;
    revoked_at?: string | null;
    created_at: string;
};

export type VerificationSessionCreateRequest = {
    site_account_id: number;
    proxy_config_id?: number | null;
    clash_node?: string;
    user_agent?: string;
    ttl_minutes?: number;
    use_account_preference?: boolean;
    operation?: SiteRecoveryOperation;
};

export type VerificationSessionCreated = {
    session: VerificationSession;
    task: VerificationTask;
};

export type VerificationBridgePairingCreated = {
    pairing: VerificationBridgePairing;
    token: string;
};

function invalidateRecovery(queryClient: ReturnType<typeof useQueryClient>) {
    queryClient.invalidateQueries({ queryKey: ['site-recovery'] });
    queryClient.invalidateQueries({ queryKey: ['sites', 'list'] });
}

function invalidateClash(queryClient: ReturnType<typeof useQueryClient>) {
    queryClient.invalidateQueries({ queryKey: ['proxy-pool', 'clash'] });
    queryClient.invalidateQueries({ queryKey: ['proxy-pool', 'list'] });
}

export function useClashControllerList(enabled = true) {
    return useQuery({
        queryKey: ['proxy-pool', 'clash', 'list'],
        queryFn: async () =>
            apiClient.get<ClashController[]>('/api/v1/proxy-pool/clash/list'),
        select: (data) => data ?? [],
        enabled,
    });
}

export function useClashControllerState(id: number | null, enabled = true) {
    return useQuery({
        queryKey: ['proxy-pool', 'clash', 'state', id],
        queryFn: async () =>
            apiClient.get<ClashGroupState>(`/api/v1/proxy-pool/clash/${id}/state`),
        enabled: enabled && typeof id === 'number' && id > 0,
        retry: false,
    });
}

export function useUpsertClashController() {
    const queryClient = useQueryClient();
    return useMutation({
        mutationFn: async (data: ClashControllerInput) =>
            apiClient.post<ClashController>('/api/v1/proxy-pool/clash/upsert', data),
        onSuccess: () => invalidateClash(queryClient),
    });
}

export function useDeleteClashController() {
    const queryClient = useQueryClient();
    return useMutation({
        mutationFn: async (id: number) =>
            apiClient.delete<null>(`/api/v1/proxy-pool/clash/${id}`),
        onSuccess: () => invalidateClash(queryClient),
    });
}

export function useSwitchClashControllerNode() {
    const queryClient = useQueryClient();
    return useMutation({
        mutationFn: async ({ id, node }: { id: number; node: string }) =>
            apiClient.post<null>(`/api/v1/proxy-pool/clash/${id}/switch`, { node }),
        onSuccess: (_, variables) => {
            queryClient.invalidateQueries({
                queryKey: ['proxy-pool', 'clash', 'state', variables.id],
            });
        },
    });
}

export function useSiteOperationAttempts(accountId: number | null, enabled = true) {
    return useQuery({
        queryKey: ['site-recovery', 'attempts', accountId],
        queryFn: async () =>
            apiClient.get<SiteOperationAttempt[]>(
                `/api/v1/site/recovery/attempts/${accountId}`,
                { limit: 100 },
            ),
        select: (data) => data ?? [],
        enabled: enabled && typeof accountId === 'number' && accountId > 0,
        refetchInterval: enabled ? 3000 : false,
    });
}

export function useSiteProxyPreferences(accountId: number | null, enabled = true) {
    return useQuery({
        queryKey: ['site-recovery', 'preferences', accountId],
        queryFn: async () =>
            apiClient.get<SiteProxyPreference[]>(
                `/api/v1/site/recovery/preferences/${accountId}`,
            ),
        select: (data) => data ?? [],
        enabled: enabled && typeof accountId === 'number' && accountId > 0,
    });
}

export function useClearAccountProxyPreference() {
    const queryClient = useQueryClient();
    return useMutation({
        mutationFn: async (accountId: number) =>
            apiClient.delete<null>(
                `/api/v1/site/recovery/preferences/account/${accountId}`,
            ),
        onSuccess: () => invalidateRecovery(queryClient),
    });
}

export function useClearSiteProxyPreference() {
    const queryClient = useQueryClient();
    return useMutation({
        mutationFn: async (siteId: number) =>
            apiClient.delete<null>(`/api/v1/site/recovery/preferences/site/${siteId}`),
        onSuccess: () => invalidateRecovery(queryClient),
    });
}

export function useVerificationSessions(accountId: number | null, enabled = true) {
    return useQuery({
        queryKey: ['site-recovery', 'verification', 'sessions', accountId],
        queryFn: async () =>
            apiClient.get<VerificationSession[]>(
                '/api/v1/site/recovery/verification',
                accountId ? { account_id: accountId } : undefined,
            ),
        select: (data) => data ?? [],
        enabled: enabled && typeof accountId === 'number' && accountId > 0,
        refetchInterval: 5000,
    });
}

export function useVerificationTasks(accountId: number | null, enabled = true) {
    return useQuery({
        queryKey: ['site-recovery', 'verification', 'tasks', accountId],
        queryFn: async () =>
            apiClient.get<VerificationTask[]>(
                '/api/v1/site/recovery/verification/tasks',
                accountId ? { account_id: accountId } : undefined,
            ),
        select: (data) => data ?? [],
        enabled: enabled && typeof accountId === 'number' && accountId > 0,
        refetchInterval: 5000,
    });
}

export function useCreateVerificationSession() {
    const queryClient = useQueryClient();
    return useMutation({
        mutationFn: async (data: VerificationSessionCreateRequest) =>
            apiClient.post<VerificationSessionCreated>(
                '/api/v1/site/recovery/verification',
                data,
            ),
        onSuccess: () => invalidateRecovery(queryClient),
    });
}

export function useCompleteVerificationSession() {
    const queryClient = useQueryClient();
    return useMutation({
        mutationFn: async ({
            sessionId,
            cookie,
            userAgent,
        }: {
            sessionId: number;
            cookie: string;
            userAgent?: string;
        }) =>
            apiClient.post<VerificationSession>(
                `/api/v1/site/recovery/verification/${sessionId}/complete`,
                {
                    cookie,
                    user_agent: userAgent?.trim() || undefined,
                },
            ),
        onSuccess: () => invalidateRecovery(queryClient),
    });
}

export function useRevokeVerificationSession() {
    const queryClient = useQueryClient();
    return useMutation({
        mutationFn: async (sessionId: number) =>
            apiClient.delete<null>(
                `/api/v1/site/recovery/verification/${sessionId}`,
            ),
        onSuccess: () => invalidateRecovery(queryClient),
    });
}

export function useClearVerificationAccount() {
    const queryClient = useQueryClient();
    return useMutation({
        mutationFn: async (accountId: number) =>
            apiClient.delete<string>(
                `/api/v1/site/recovery/verification/account/${accountId}`,
            ),
        onSuccess: () => invalidateRecovery(queryClient),
    });
}

export function useRetryVerificationOperation() {
    const queryClient = useQueryClient();
    return useMutation({
        mutationFn: async (sessionId: number) =>
            apiClient.post<VerificationRetryStatus>(
                `/api/v1/site/recovery/verification/${sessionId}/retry`,
            ),
        onSuccess: () => invalidateRecovery(queryClient),
    });
}

export function useVerificationPairings(enabled = true) {
    return useQuery({
        queryKey: ['site-recovery', 'verification', 'pairings'],
        queryFn: async () =>
            apiClient.get<VerificationBridgePairing[]>(
                '/api/v1/site/recovery/verification/pairings',
            ),
        select: (data) => data ?? [],
        enabled,
    });
}

export function useCreateVerificationPairing() {
    const queryClient = useQueryClient();
    return useMutation({
        mutationFn: async (data: { name: string; ttl_days?: number }) =>
            apiClient.post<VerificationBridgePairingCreated>(
                '/api/v1/site/recovery/verification/pairings',
                data,
            ),
        onSuccess: () => {
            queryClient.invalidateQueries({
                queryKey: ['site-recovery', 'verification', 'pairings'],
            });
        },
    });
}

export function useRevokeVerificationPairing() {
    const queryClient = useQueryClient();
    return useMutation({
        mutationFn: async (pairingId: number) =>
            apiClient.delete<null>(
                `/api/v1/site/recovery/verification/pairings/${pairingId}`,
            ),
        onSuccess: () => {
            queryClient.invalidateQueries({
                queryKey: ['site-recovery', 'verification'],
            });
        },
    });
}
