import type {
    ProtocolFeature,
    ProtocolName,
    ProtocolPolicy,
    RouteCandidateStatus,
    RoutingStrategy,
} from '@/api/endpoints/model-catalog';

export const ROUTING_STRATEGIES: RoutingStrategy[] = [
    'balanced',
    'reliability',
    'lowest-cost',
    'lowest-latency',
    'manual',
];

export const CANONICAL_PROTOCOL_POLICIES: Exclude<ProtocolPolicy, 'inherit'>[] = [
    'auto',
    'passthrough-only',
    'transform-allowed',
];

export const CANDIDATE_PROTOCOL_POLICIES: ProtocolPolicy[] = [
    'inherit',
    'auto',
    'passthrough-only',
    'transform-allowed',
];

export const CANDIDATE_STATUSES: RouteCandidateStatus[] = [
    'active',
    'degraded',
    'stale',
    'unavailable',
    'disabled',
    'archived',
];

export const INBOUND_PROTOCOLS: ProtocolName[] = [
    'openai_chat',
    'openai_responses',
    'anthropic',
    'openai_embedding',
];

export const PROTOCOL_FEATURES: ProtocolFeature[] = [
    'function_tools',
    'built_in_tools',
    'structured_output',
    'images',
    'audio',
    'files',
    'documents',
    'reasoning',
    'cache_control',
    'continuation',
    'responses_state',
    'provider_extensions',
    'anthropic_mcp',
    'anthropic_container',
    'anthropic_server_tools',
    'gemini_extensions',
    'websocket',
];
