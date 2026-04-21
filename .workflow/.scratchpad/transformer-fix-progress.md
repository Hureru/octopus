# 三大供应商转换器修复进度

> 配套文件：[`transformer-fix-plan.md`](./transformer-fix-plan.md)  
> 更新规则：完成一条即更新 `状态` 与 `完成时间` / `commit`；卡住的项在 `备注` 列说明阻塞原因。

## 状态图例

- ⏸ Pending（未开始）
- 🔄 In Progress
- ✅ Done
- ❌ Blocked（需人工介入，`备注`说明）
- ⛔ Skipped（评估后决定不做，`备注`说明）

---

## P1 - 止血

| 编号 | 标题 | 状态 | 完成时间 | Commit | 备注 |
|------|------|------|----------|--------|------|
| G-C1 | `responseSchema.type` 大写 | ✅ | 2026-04-21 | 7b85e07 | MarshalJSON 方案，结构体字段保持 Draft-07 小写 |
| G-C2 | `functionResponse.name` 应为函数名 | ✅ | 2026-04-21 | b61d5ce | 优先 ToolCallName，否则按 ID 反查 assistant.ToolCalls；G-H7 另行做完整 ReasoningBlock 扩展 |
| G-C3 | `systemInstruction` camelCase + role omitempty | ✅ | 2026-04-21 | 5ba1d59 | |
| G-C5 | `thinkingBudget` 按家族 clamp | ✅ | 2026-04-21 | aec360b | 细分 25Flash/25Pro/3，Pro 禁 0 → 128，Gemini 3 转 level |
| A-C1 | `code_execution_tool_result` 类型保留 | ✅ | 2026-04-21 | 669295e | 在 ServerToolResultBlock 增 BlockType，inbound 写入、outbound 读出 |
| A-C3 | 1h cache ttl 自动加 beta 头 | ✅ | 2026-04-21 | 9cf1c18 | 新增 collectAnthropicBetaHeaders，为 A-H5 的 server tools 头复用 |
| O-C1 | 非法 finish_reason `"error"` | ✅ | 2026-04-21 | 6995f17 | 抽 normalizeResponsesFinishReason，流式与非流式双路覆盖 |
| O-C2 | 流式 tool_calls 被错标 stop | ✅ | 2026-04-21 | 9db70ef | responseCarriesFunctionCall 在 completed 后覆写 stop → tool_calls |
| O-C4 | `PromptCacheKey` 改 string + Chat 出站透传 | ✅ | 2026-04-21 | 3312354 | 修类型为 *string，Chat 白名单补字段 |

## P2 - 补能力

| 编号 | 标题 | 状态 | 完成时间 | Commit | 备注 |
|------|------|------|----------|--------|------|
| O-C3 | Responses 内置工具不丢弃 | ✅ | 2026-04-21 | ada6701 | 采用 raw passthrough 方案：inbound 打标 → relay 强制路由到 Responses channel（否则 400）→ outbound TransformRequestRaw 保留原始 body。跨供应商场景明确拒绝而非静默丢弃 |
| A-C2 | 流式 error 事件上报 | ✅ | 2026-04-21 | 801d892 | StreamEvent 增 Error 字段；outbound 新增 error case + mapAnthropicErrorTypeToStatus；inbound 直接发 `event: error` SSE |
| G-C4 | 流式 ReasoningBlock.Index 全局有序 | ✅ | 2026-04-21 | fe6ae8a | MessagesOutbound 加 per-candidate streamReasoningIndex；与 G-H7 合并实施 |
| A-H5 | server tools 在 inbound 保留 + 自动加 beta 头 | ✅ | 2026-04-21 | e92c993 | inbound Tool 恢复 Type + RawBody + Marshal/Unmarshal；outbound convertTools 识别 server tool 透传；复用 collectAnthropicBetaHeaders 加 web-search/code-execution/computer-use beta |
| O-H1 | Chat 白名单补 2025 字段 | ✅ | 2026-04-21 | 2d2b2e6 | 补 user/verbosity/prediction/web_search_options；InternalLLMRequest 同步新字段 |
| O-H4 | Responses 流式 refusal 事件 | ✅ | 2026-04-21 | 832b6c0 | outbound 处理 response.refusal.delta → Choice.Delta.Refusal；done 丢弃避免双重累加 |

## P3 - 计费与元数据

| 编号 | 标题 | 状态 | 完成时间 | Commit | 备注 |
|------|------|------|----------|--------|------|
| A-H1 | `stop_sequence` 双向透传 | ⏸ | | | |
| A-H2 | 流式 TotalTokens 含缓存 | ⏸ | | | |
| G-H2 | Gemini response 元数据补齐 | ⏸ | | | |
| G-H3 | UsageMetadata 新字段 | ⏸ | | | |
| O-H3 | `response.completed.output` 非空 | ⏸ | | | |
| O-H5 | `truncation` 字段 | ⏸ | | | |

## P4 - 参数 / 边界 / 安全

| 编号 | 标题 | 状态 | 完成时间 | Commit | 备注 |
|------|------|------|----------|--------|------|
| A-H3 | Anthropic top_k / service_tier 透传 | ⏸ | | | 合并 A-M1 |
| A-H4 | thinking 时参数约束 | ⏸ | | | |
| G-H1 | GenerationConfig 缺参数 | ⏸ | | | |
| G-H5 | baseUrl 缺 `/v1beta` 兜底 | ⏸ | | | |
| G-H6 | API key 改用请求头 | ⏸ | | | |
| G-H7 | 多并发 thoughtSignature 按名配对 | ✅ | 2026-04-21 | fe6ae8a | ReasoningBlock 加 ToolCallID/ToolCallName；outbound 新增 collectGeminiSignaturesByName 优先按 name 查找；随 G-C4 合并 |
| O-H2 | Chat 入站设 RawAPIFormat | ⏸ | | | |
| O-H6 | Responses 多模态输入补齐 | ⏸ | | | |

## P5 - Medium

| 编号 | 状态 | Commit | 备注 |
|------|------|--------|------|
| A-M2 | ⏸ | | |
| A-M3 | ⏸ | | |
| A-M4 | ⏸ | | |
| A-M5 | ⏸ | | |
| A-M6 | ⏸ | | |
| G-M1 | ⏸ | | |
| G-M2 | ⏸ | | |
| G-M3 | ⏸ | | |
| G-M4 | ⏸ | | |
| G-M5 | ⏸ | | |
| G-M6 | ⏸ | | |
| G-M7 | ⏸ | | |
| O-M1 | ⏸ | | |
| O-M2 | ✅ | ada6701 | 随 O-C3 直通方案自然解决：raw body 直通下流式 SSE 原样转发，annotations.added / file_search_call.* / web_search_call.* / code_interpreter_call.* / mcp_call.* 等事件不再被 outbound switch 的 default 分支吞掉 |
| O-M3 | ⏸ | | |
| O-M4 | ⏸ | | |
| O-M5 | ⏸ | | |
| O-M6 | ⏸ | | |

## P6 - Low

| 编号 | 状态 | Commit | 备注 |
|------|------|--------|------|
| A-L1 | ⏸ | | |
| A-L3 | ⏸ | | |
| A-L4 | ⏸ | | |
| G-L2 | ⏸ | | |
| G-L5 | ⏸ | | |
| O-L1 | ⏸ | | |
| O-L2 | ⏸ | | |
| O-L3 | ⏸ | | |
| O-L4 | ⏸ | | |

---

## 变更日志

| 日期 | 变更 | 作者 |
|------|------|------|
| 2026-04-21 | 初始化计划与进度文件 | Claude Code (审查报告) |
| 2026-04-21 | 标记 O-C3 / O-M2 已解决（commit ada6701 引入 OpenAI Responses 原生直通） | Hureru |
| 2026-04-21 | P1 批次全部完成（G-C1 7b85e07 / G-C2 b61d5ce / G-C3 5ba1d59 / G-C5 aec360b / A-C1 669295e / A-C3 9cf1c18 / O-C1 6995f17 / O-C2 9db70ef / O-C4 3312354） | Claude Code |
| 2026-04-21 | P2 批次全部完成（A-C2 801d892 / O-H1 2d2b2e6 / O-H4 832b6c0 / A-H5 e92c993 / G-C4+G-H7 fe6ae8a） | Claude Code |

---

## 实施指引

1. 开始一条任务前：
   - `TaskUpdate` 对应任务为 `in_progress`
   - 本文件把 `状态` 改为 🔄
2. 完成一条任务后：
   - 填写 `完成时间`、`Commit`，状态改为 ✅
   - 提交信息格式：`fix(transformer-{vendor}): {{短述}} ({{编号}})`
   - 单测同 PR 提交
3. 遇到阻塞：
   - 状态改 ❌，`备注`列写明原因和所需决策
   - 不要跳过重新排序执行；继续做同批次其他不阻塞项
4. 每批完成后：
   - 在变更日志追加一行
   - 开 PR 关联本批次所有条目
