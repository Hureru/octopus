import type { HeaderPolicyScope } from '@/api/endpoints/header-policy';

export type ScopeTarget = {
    id: number;
    label: string;
};

export type ScopeTargets = Record<HeaderPolicyScope, ScopeTarget[]>;

export const HEADER_POLICY_SCOPES: HeaderPolicyScope[] = [
    'global',
    'site',
    'site_account',
    'channel',
    'canonical_model',
    'route_candidate',
];
