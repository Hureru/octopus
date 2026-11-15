import type { RelayLog } from '@/api/endpoints/log';

export interface LogRow {
    log: RelayLog;
    /** 该行属于一叠连续失败；head 行负责渲染展开 / 收起按钮 */
    stack?: { ids: number[]; expanded: boolean; head: boolean };
}

// 熔断 / 全部跳过导致的失败没有 error 文本，只能靠 success 判断；旧版本后端没有这个字段时退回 error。
const isFailed = (log: RelayLog) => !log.pending && (log.success === false || !!log.error);

/**
 * 把相邻、都失败、请求模型与 API Key 都相同的日志折成一叠。
 * 折叠时整叠只出一行（最新一条）；任一成员在 expanded 里就整叠展开，
 * 这样新失败并入或翻页并入更老的失败时，展开状态不会丢。
 */
export function groupLogRows(logs: RelayLog[], expanded: ReadonlySet<number>): LogRow[] {
    const rows: LogRow[] = [];
    for (let i = 0; i < logs.length; ) {
        const head = logs[i];
        let j = i + 1;
        if (isFailed(head)) {
            while (
                j < logs.length
                && isFailed(logs[j])
                && logs[j].request_model_name === head.request_model_name
                && (logs[j].request_api_key_name ?? '') === (head.request_api_key_name ?? '')
            ) j++;
        }
        if (j - i < 2) {
            rows.push({ log: head });
        } else {
            const run = logs.slice(i, j);
            const ids = run.map((log) => log.id);
            if (ids.some((id) => expanded.has(id))) {
                run.forEach((log, k) => rows.push({ log, stack: { ids, expanded: true, head: k === 0 } }));
            } else {
                rows.push({ log: head, stack: { ids, expanded: false, head: true } });
            }
        }
        i = j;
    }
    return rows;
}
