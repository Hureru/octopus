import type { Locale } from '@/stores/setting';

type SiteMessageTranslator = (key: string, values?: Record<string, string | number>) => string;

type MatchResult = {
    key: string;
    values?: Record<string, string | number>;
};

const missingGroupKeyNoUsableKeyPattern = /^site sync requires a key for group "([^"]+)"; create a key for that group on the site and sync again$/i;
const missingGroupKeyFallbackPattern = /^site sync could not resolve models for group "([^"]+)"; create a key for that group on the site and sync again$/i;
// Legacy compatibility for responses/log messages emitted before coded API errors were introduced.
const exactSiteMessageKeys: Record<string, string> = {
    'invalid json format': 'siteImport.errors.invalidJson',
    'site import invalid json': 'siteImport.errors.invalidJson',
    'site import empty payload': 'siteImport.errors.emptyPayload',
    'site import no recognizable all api hub payload sections': 'siteImport.errors.unrecognizedAllAPIHub',
    'site import no recognizable metapi accounts section': 'siteImport.errors.unrecognizedMetapi',
    'site import no importable all api hub site account data': 'siteImport.errors.noImportableAllAPIHub',
    'site import no importable metapi site account data': 'siteImport.errors.noImportableMetapi',
};

function matchSiteMessage(message: string): MatchResult | null {
    const trimmed = message.trim();
    if (!trimmed) {
        return null;
    }

    const exactKey = exactSiteMessageKeys[trimmed.toLowerCase()];
    if (exactKey) {
        return { key: exactKey };
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
            key: 'siteSync.errors.missingGroupModelsOrKey',
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
        case 'siteSync.errors.missingGroupModelsOrKey':
            switch (locale) {
                case 'en':
                    return 'Failed to fetch models: the current group has no available models or key.';
                case 'zh_hant':
                    return '獲取模型失敗：當前分組沒有可用模型或可用 Key';
                case 'zh_hans':
                default:
                    return '获取模型失败：当前分组没有可用模型或可用 Key';
            }
        case 'siteImport.errors.invalidJson':
            switch (locale) {
                case 'en':
                    return 'The import content is not valid JSON. Check the file format or pasted content.';
                case 'zh_hant':
                    return '匯入內容不是有效的 JSON，請檢查檔案格式或貼上的內容。';
                case 'zh_hans':
                default:
                    return '导入内容不是有效的 JSON，请检查文件格式或粘贴内容。';
            }
        case 'siteImport.errors.emptyPayload':
            switch (locale) {
                case 'en':
                    return 'The import content is empty. Select a JSON file or paste exported content.';
                case 'zh_hant':
                    return '匯入內容為空，請選擇 JSON 檔案或貼上匯出內容。';
                case 'zh_hans':
                default:
                    return '导入内容为空，请选择 JSON 文件或粘贴导出内容。';
            }
        case 'siteImport.errors.unrecognizedAllAPIHub':
            switch (locale) {
                case 'en':
                    return 'No All API Hub export data was recognized. Make sure you selected the correct exported JSON.';
                case 'zh_hant':
                    return '未識別到 All API Hub 匯出資料，請確認選擇了正確的匯出 JSON。';
                case 'zh_hans':
                default:
                    return '未识别到 All API Hub 导出数据，请确认选择了正确的导出 JSON。';
            }
        case 'siteImport.errors.unrecognizedMetapi':
            switch (locale) {
                case 'en':
                    return 'No Metapi account export data was recognized. Make sure you selected the correct exported JSON.';
                case 'zh_hant':
                    return '未識別到 Metapi 帳號匯出資料，請確認選擇了正確的匯出 JSON。';
                case 'zh_hans':
                default:
                    return '未识别到 Metapi 账号导出数据，请确认选择了正确的导出 JSON。';
            }
        case 'siteImport.errors.noImportableAllAPIHub':
            switch (locale) {
                case 'en':
                    return 'No importable All API Hub site account data was found.';
                case 'zh_hant':
                    return '未找到可匯入的 All API Hub 站點帳號資料。';
                case 'zh_hans':
                default:
                    return '未找到可导入的 All API Hub 站点账号数据。';
            }
        case 'siteImport.errors.noImportableMetapi':
            switch (locale) {
                case 'en':
                    return 'No importable Metapi site account data was found.';
                case 'zh_hant':
                    return '未找到可匯入的 Metapi 站點帳號資料。';
                case 'zh_hans':
                default:
                    return '未找到可导入的 Metapi 站点账号数据。';
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
