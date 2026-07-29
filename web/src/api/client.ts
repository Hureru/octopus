import { translateApiErrorCode } from './error-i18n';
import type { ApiError, ApiErrorParams } from './types';
import { HttpStatus } from './types';

export const API_BASE_URL = process.env.NEXT_PUBLIC_API_BASE_URL || '.';

/**
 * 获取认证 Store（延迟导入以避免循环依赖）
 */
let getAuthStore: (() => { token: string | null; logout: () => void }) | null = null;

export function setAuthStoreGetter(getter: () => { token: string | null; logout: () => void }) {
    getAuthStore = getter;
}

/**
 * 全局错误处理
 */
const handleError = (error: ApiError) => {
    console.error('API Error:', error);

    // 401 未授权，调用 store 的 logout
    if (error.code === HttpStatus.UNAUTHORIZED) {
        if (getAuthStore) {
            const store = getAuthStore();
            store.logout();
        }
    }
};

function isApiErrorParams(value: unknown): value is ApiErrorParams {
    if (!value || typeof value !== 'object' || Array.isArray(value)) {
        return false;
    }
    return Object.values(value).every((item) => (
        item === null ||
        item === undefined ||
        typeof item === 'string' ||
        typeof item === 'number' ||
        typeof item === 'boolean'
    ));
}

/**
 * 处理响应
 */
async function handleResponse<T>(response: Response): Promise<T> {
    const contentType = response.headers.get('content-type');
    const isJson = contentType?.includes('application/json');

    let data: unknown;
    if (isJson) {
        data = await response.json();
    } else {
        data = await response.text();
    }

    if (!response.ok) {
        const rawMessage = (data && typeof data === 'object' && 'message' in data && typeof data.message === 'string')
            ? data.message
            : (typeof data === 'string' ? data : response.statusText);
        const errorCode = (data && typeof data === 'object' && 'error_code' in data && typeof data.error_code === 'string')
            ? data.error_code
            : undefined;
        const errorParams = (data && typeof data === 'object' && 'params' in data && isApiErrorParams(data.params))
            ? data.params
            : undefined;
        const error: ApiError = {
            code: response.status,
            errorCode,
            rawMessage,
            params: errorParams,
            message: translateApiErrorCode(errorCode, rawMessage, errorParams),
        };

        handleError(error);
        throw error;
    }

    // 如果是标准的 ApiResponse 格式，返回 data 字段
    if (data && typeof data === 'object' && 'data' in data) {
        return data.data as T;
    }

    return data as T;
}

/**
 * 发送请求
 */
async function request<T>(
    method: string,
    path: string,
    body?: BodyInit,
    params?: Record<string, string | number | boolean>
): Promise<T> {
    // 构建 URL
    const searchParams = params ? new URLSearchParams(
        Object.entries(params).map(([k, v]) => [k, String(v)])
    ).toString() : '';
    const url = `${API_BASE_URL}${path}${searchParams ? `?${searchParams}` : ''}`;

    // 构建请求头
    const headers = new Headers();

    // 只在有 body 时设置 Content-Type
    if (body) {
        headers.set('Content-Type', 'application/json');
    }

    // 添加 Authorization - 从 zustand store 获取 token
    if (typeof window !== 'undefined' && getAuthStore) {
        const store = getAuthStore();
        if (store.token) {
            headers.set('Authorization', `Bearer ${store.token}`);
        }
    }

    // 发送请求
    const response = await fetch(url.toString(), {
        method,
        headers,
        body,
    });

    return handleResponse<T>(response);
}

function parseDownloadFilename(contentDisposition: string | null): string | null {
    if (!contentDisposition) return null;
    const encoded = contentDisposition.match(/filename\*=UTF-8''([^;]+)/i)?.[1];
    if (encoded) {
        try {
            return decodeURIComponent(encoded);
        } catch {
            // Fall through to the plain filename parameter.
        }
    }
    return contentDisposition.match(/filename="([^"]+)"/i)?.[1] ?? null;
}

function safeDownloadFilename(value: string) {
    return value.replace(/[\\/\u0000-\u001f\u007f]/g, '_');
}

function triggerBlobDownload(blob: Blob, filename: string) {
    const url = URL.createObjectURL(blob);
    try {
        const anchor = document.createElement('a');
        anchor.href = url;
        anchor.download = safeDownloadFilename(filename);
        document.body.appendChild(anchor);
        anchor.click();
        anchor.remove();
    } finally {
        window.setTimeout(() => URL.revokeObjectURL(url), 1000);
    }
}

export async function downloadApiFile(path: string, fallbackFilename: string) {
    const headers = new Headers();
    const token = getAuthStore?.().token;
    if (!token) throw new Error('Not authenticated');
    headers.set('Authorization', `Bearer ${token}`);

    const response = await fetch(`${API_BASE_URL}${path}`, {
        method: 'GET',
        headers,
    });
    if (!response.ok) {
        await handleResponse<never>(response);
        throw new Error(response.statusText);
    }

    const filename =
        parseDownloadFilename(response.headers.get('content-disposition')) || fallbackFilename;
    triggerBlobDownload(await response.blob(), filename);
    return { filename: safeDownloadFilename(filename) };
}

/**
 * API 客户端 - 基础 HTTP 方法
 */
export const apiClient = {
    /**
     * GET 请求
     */
    get: <T>(path: string, params?: Record<string, string | number | boolean>): Promise<T> =>
        request<T>('GET', path, undefined, params),

    /**
     * POST 请求
     */
    post: <T>(path: string, data?: unknown, params?: Record<string, string | number | boolean>): Promise<T> =>
        request<T>('POST', path, data ? JSON.stringify(data) : undefined, params),

    /**
     * PUT 请求
     */
    put: <T>(path: string, data?: unknown, params?: Record<string, string | number | boolean>): Promise<T> =>
        request<T>('PUT', path, data ? JSON.stringify(data) : undefined, params),

    /**
     * DELETE 请求
     */
    delete: <T>(path: string, params?: Record<string, string | number | boolean>): Promise<T> =>
        request<T>('DELETE', path, undefined, params),

    /**
     * PATCH 请求
     */
    patch: <T>(path: string, data?: unknown, params?: Record<string, string | number | boolean>): Promise<T> =>
        request<T>('PATCH', path, data ? JSON.stringify(data) : undefined, params),
};
