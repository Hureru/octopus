import type { Locale } from '@/stores/setting';

type SiteMessageTranslator = (key: string, values?: Record<string, string | number>) => string;

type MatchResult = {
    key: string;
    values?: Record<string, string | number>;
};

const missingGroupKeyNoUsableKeyPattern = /^site sync requires a key for group "([^"]+)"; create a key for that group on the site and sync again$/i;
const missingGroupKeyFallbackPattern = /^site sync could not resolve models for group "([^"]+)"; create a key for that group on the site and sync again$/i;

function matchSiteMessage(message: string): MatchResult | null {
    const trimmed = message.trim();
    if (!trimmed) {
        return null;
    }

    const noUsableKeyMatch = trimmed.match(missingGroupKeyNoUsableKeyPattern);
    if (noUsableKeyMatch) {
        return {
            key: 'siteSync.errors.missingGroupKey',
            values: { groupKey: noUsableKeyMatch[1] },
        };
    }

    const fallbackMatch = trimmed.match(missingGroupKeyFallbackPattern);
    if (fallbackMatch) {
        return {
            key: 'siteSync.errors.missingGroupKey',
            values: { groupKey: fallbackMatch[1] },
        };
    }

    return null;
}

function interpolate(template: string, values?: Record<string, string | number>) {
    if (!values) {
        return template;
    }
    return template.replace(/\{(\w+)\}/g, (_, key: string) => String(values[key] ?? `{${key}}`));
}

function fallbackTranslate(locale: Locale, key: string, values?: Record<string, string | number>) {
    switch (key) {
        case 'siteSync.errors.missingGroupKey':
            switch (locale) {
                case 'en':
                    return interpolate('Group "{groupKey}" has no available key. Create a key for this group on the site and sync again.', values);
                case 'zh_hant':
                    return interpolate('分組「{groupKey}」沒有可用的 Key。請先到站點建立這個分組的 Key，再重新同步。', values);
                case 'zh_hans':
                default:
                    return interpolate('分组「{groupKey}」没有可用的 Key。请先到站点创建这个分组的 Key，再重新同步。', values);
            }
        default:
            return '';
    }
}

export function translateSiteMessage(
    locale: Locale,
    message: string | null | undefined,
    t?: SiteMessageTranslator,
) {
    const raw = typeof message === 'string' ? message.trim() : '';
    if (!raw) {
        return '';
    }

    const matched = matchSiteMessage(raw);
    if (!matched) {
        return raw;
    }

    if (t) {
        const translated = t(matched.key, matched.values);
        if (translated && translated !== matched.key) {
            return translated;
        }
    }

    return fallbackTranslate(locale, matched.key, matched.values) || raw;
}

