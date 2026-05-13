import { useSettingStore, type Locale } from '@/stores/setting';

const zhHansErrors = {
    site: {
        sub2api: {
            api_key_required: 'Sub2API 同步需要 API Key。请先在站点创建一个 Key 后再重新同步。',
            model_api_key_required: 'Sub2API 模型发现需要 API Key。请先在站点创建一个 Key 后再重新同步。',
            envelope_failed: 'Sub2API 返回错误，请检查 Access Token、权限或站点状态。',
            missing_data: 'Sub2API 响应缺少必要数据，请检查站点兼容性或稍后重试。',
        },
    },
};

const zhHantErrors = {
    site: {
        sub2api: {
            api_key_required: 'Sub2API 同步需要 API Key。請先在站點建立一個 Key 後再重新同步。',
            model_api_key_required: 'Sub2API 模型發現需要 API Key。請先在站點建立一個 Key 後再重新同步。',
            envelope_failed: 'Sub2API 返回錯誤，請檢查 Access Token、權限或站點狀態。',
            missing_data: 'Sub2API 回應缺少必要資料，請檢查站點相容性或稍後重試。',
        },
    },
};

const enErrors = {
    site: {
        sub2api: {
            api_key_required: 'Sub2API sync requires an API key. Create a key on the site and sync again.',
            model_api_key_required: 'Sub2API model discovery requires an API key. Create a key on the site and sync again.',
            envelope_failed: 'Sub2API returned an error. Check the access token, permissions, or site status.',
            missing_data: 'Sub2API response is missing required data. Check site compatibility or try again later.',
        },
    },
};

const errorMessages: Record<Locale, unknown> = {
    zh_hans: zhHansErrors,
    zh_hant: zhHantErrors,
    en: enErrors,
};

type ErrorValues = Record<string, string | number>;

function getErrorMessageFallbacks(locale: Locale): unknown[] {
    if (locale.startsWith('en')) {
        return [errorMessages.en, errorMessages.zh_hans];
    }
    if (locale === 'zh_hant') {
        return [errorMessages.zh_hant, errorMessages.zh_hans, errorMessages.en];
    }
    if (locale === 'zh_hans') {
        return [errorMessages.zh_hans, errorMessages.en];
    }
    return [errorMessages.en, errorMessages.zh_hans];
}

function lookupMessage(source: unknown, path: string): string | null {
    let current: unknown = source;
    for (const part of path.split('.')) {
        if (!current || typeof current !== 'object' || Array.isArray(current)) {
            return null;
        }
        current = (current as Record<string, unknown>)[part];
    }
    return typeof current === 'string' ? current : null;
}

function interpolate(template: string, values?: ErrorValues): string {
    if (!values) return template;
    return template.replace(/\{(\w+)\}/g, (_, key: string) => String(values[key] ?? `{${key}}`));
}

export function translateApiErrorCode(
    errorCode: string | null | undefined,
    fallback: string,
    values?: ErrorValues,
): string {
    const normalizedCode = typeof errorCode === 'string' ? errorCode.trim() : '';
    if (!normalizedCode) return fallback;

    const locale = useSettingStore.getState().locale;
    const translated = getErrorMessageFallbacks(locale)
        .map((source) => lookupMessage(source, normalizedCode))
        .find((message): message is string => Boolean(message));

    return translated ? interpolate(translated, values) : fallback;
}
