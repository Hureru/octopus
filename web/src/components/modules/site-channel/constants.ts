import type { SiteModelRouteSource, SiteModelRouteType } from '@/api/endpoints/site-channel';
import { ChannelType } from '@/api/endpoints/channel';

export const SITE_ROUTE_COLUMN_ORDER: SiteModelRouteType[] = [
    'openai_chat',
    'openai_response',
    'anthropic',
    'gemini',
    'volcengine',
    'openai_embedding',
];

export const SITE_ROUTE_DISPLAY_ORDER: SiteModelRouteType[] = [
    ...SITE_ROUTE_COLUMN_ORDER,
    'unknown',
];

export const SITE_ROUTE_TO_CHANNEL_TYPE: Record<Exclude<SiteModelRouteType, 'unknown'>, ChannelType> = {
    openai_chat: ChannelType.OpenAIChat,
    openai_response: ChannelType.OpenAIResponse,
    anthropic: ChannelType.Anthropic,
    gemini: ChannelType.Gemini,
    volcengine: ChannelType.Volcengine,
    openai_embedding: ChannelType.OpenAIEmbedding,
};

export const ROUTE_COLUMN_KEY_PREFIX = 'site-route-column';

export function getRouteTypeTone(routeType: SiteModelRouteType) {
    switch (routeType) {
        case 'unknown':
            return 'border-amber-500/30 bg-amber-500/10 text-amber-700 dark:text-amber-300';
        case 'anthropic':
            return 'border-orange-500/30 bg-orange-500/10 text-orange-700 dark:text-orange-300';
        case 'gemini':
            return 'border-emerald-500/30 bg-emerald-500/10 text-emerald-700 dark:text-emerald-300';
        case 'volcengine':
            return 'border-cyan-500/30 bg-cyan-500/10 text-cyan-700 dark:text-cyan-300';
        case 'openai_embedding':
            return 'border-fuchsia-500/30 bg-fuchsia-500/10 text-fuchsia-700 dark:text-fuchsia-300';
        case 'openai_response':
            return 'border-blue-500/30 bg-blue-500/10 text-blue-700 dark:text-blue-300';
        default:
            return 'border-primary/20 bg-primary/10 text-primary';
    }
}

export function isSupportedRouteType(routeType: SiteModelRouteType): routeType is Exclude<SiteModelRouteType, 'unknown'> {
    return routeType !== 'unknown';
}

export function getRouteSourceTone(routeSource: SiteModelRouteSource) {
    switch (routeSource) {
        case 'manual_override':
            return 'border-primary/30 bg-primary/10 text-primary';
        case 'runtime_learned':
            return 'border-sky-500/30 bg-sky-500/10 text-sky-700 dark:text-sky-300';
        case 'default_assigned':
            return 'border-amber-500/30 bg-amber-500/10 text-amber-700 dark:text-amber-300';
        default:
            return 'border-border bg-muted/40 text-muted-foreground';
    }
}
