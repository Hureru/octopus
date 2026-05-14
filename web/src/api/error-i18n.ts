import enLocale from '../../public/locale/en.json';
import zhHansLocale from '../../public/locale/zh_hans.json';
import zhHantLocale from '../../public/locale/zh_hant.json';
import { useSettingStore, type Locale } from '@/stores/setting';

export type ErrorValues = Record<string, string | number | boolean | null | undefined>;

const errorMessages: Record<Locale, unknown> = {
    zh_hans: zhHansLocale.errors,
    zh_hant: zhHantLocale.errors,
    en: enLocale.errors,
};

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
    return template.replace(/\{(\w+)\}/g, (_, key: string) => {
        const value = values[key];
        return value === undefined || value === null ? `{${key}}` : String(value);
    });
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
