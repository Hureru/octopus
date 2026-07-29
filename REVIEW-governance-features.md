# Octopus 治理特性最终复核

- **复核日期**：2026-07-29
- **范围**：ADR 0001-0013、父任务与八个子任务的当前工作树
- **工具链**：`go1.25.0 darwin/amd64`、`corepack pnpm`
- **结论**：原始报告中的实现正确性问题已关闭，或已明确归类为保留设计/接受风险。当前没有仍开放的 P0/P1 正确性 finding。
- **证据边界**：SQLite、Go、前端和浏览器验收已动态执行；MySQL/PostgreSQL 只完成静态方言核对，未宣称动态通过。

## 最终状态矩阵

| Finding | 状态 | 当前结论 |
|---|---|---|
| P0-1 | 已修复 | 显式代理优先，direct 只作 fallback，且 direct 成功不覆盖学习偏好。 |
| P0-2 | 已修复 | `original_error` 使用读入 Go 的旧值写回，不依赖 MySQL `SET` 求值顺序。 |
| P0-3 | 已修复 | relay log、request/attempt facts 与 hour/day aggregates 在同一修复事务内同步；缺 daily snapshot 时回滚。 |
| P0-4 | 已修复 | AnyRouter 请求接入受信 Verification Cookie/UA，并保持最小 Cookie 合并。 |
| P0-5 | 已修复 | 多层 Protocol Policy 取更严格值，`allowLossy` 取 AND。 |
| P0-6 | 已修复 | Anthropic/Volcengine 结构化输出损耗进入显式 capability/warning 边界。 |
| P0-7 | 已修复 | Volcengine 使用真实协议与 execution mode，不再伪装 OpenAI passthrough。 |
| P1-1 | 已修复 | Pairing 是账号级 capability，只能领取其账号任务；旧无 scope pairing 被迁移撤销并在运行时拒绝。 |
| P1-2 | 已修复 | `x-goog-api-key` 纳入 protected header，受信凭证最后注入。 |
| P1-3 | 已修复 | 扩展和服务端仅接受 Cloudflare Cookie 白名单。 |
| P1-4 | 接受风险 | 本地密钥仍由数据库内 JWT secret 派生；真正消除该风险需要外部 KMS、轮换和兼容迁移，不以破坏现有安装的方式伪修。 |
| P1-5 | 已修复 | 站点偏好清除参数与账号边界已有回归测试。 |
| P1-6 | 已修复 | 账号级偏好可建立并优先于站点级偏好。 |
| P1-7 | 已修复 | 账号偏好不可用时回退站点偏好。 |
| P1-8 | 已修复 | Clash 进程 guard + DB lease 覆盖调用方请求整个生命周期。 |
| P1-9 | 已修复 | Controller client 固定 15 秒超时，guard 等待响应 context cancel。 |
| P1-10 | 已修复 | pricing 在恢复成功路径快照、ProjectAccount 和 CatalogSync 之后执行。 |
| P1-11 | 已修复 | 429/503 Cloudflare challenge 被识别并暂停盲目重试。 |
| P1-12 | 已修复 | HTTP 200 HTML challenge 进入验证路径。 |
| P1-13 | 已修复 | Cookie 采用合并而非覆盖，受信验证 Cookie 最后处理。 |
| P1-14 | 已修复 | 近期查询读取小时聚合，长期查询读取日聚合。 |
| P1-15 | 已修复 | `usage_hourly_retention_days` 控制真实小时聚合清理。 |
| P1-16 | 已修复 | facts 仅在已聚合、过期且 daily snapshot 完整时删除。 |
| P1-17 | 已修复 | 上游 WS 自身超时与客户端取消分开归责。 |
| P1-18 | 已修复 | aggregate 同键同指标幂等；同键不同指标整笔回滚；创建后无条件重读校验。 |
| P1-19 | 已修复 | GroupItem 写入立即投影 Route Candidate。 |
| P1-20 | 已修复 | non-manual 由协议/策略分数主导，Priority 仅 tie-break；manual 才由 Priority 主导。 |
| P1-21 | 已修复 | passthrough-only 按真实执行能力判定，Embedding/continuation/WS 不再因协议名误判。 |
| P2-1 | 已修复 | 计费与预览共用 `internal/globalprice` Provider。 |
| P2-2 | 已修复 | lowest-cost 排除 per-request 和不可换算价格。 |
| P2-3 | 已修复 | Candidate 投影完成后刷新报价，不生成 candidate=0 孤儿报价。 |
| P2-4 | 已修复 | manual quote 显式绑定 Candidate。 |
| P2-5 | 已修复 | 过期 manual 不覆盖新鲜报价。 |
| P2-6 | 已修复 | 列表与解析器均包含 Candidate 可继承的 site-wide 报价。 |
| P2-7 | 已修复 | Compact、Images、WS transform/passthrough 均传 Canonical/Candidate scope，并记录 trace；WS pool 按最终 header signature 隔离。 |
| P2-8 | 已修复 | 显式空字符串可清除继承 User-Agent。 |
| P2-9 | 已修复 | Header Policy 按 ID 或 scope/scope_id 更新，不触发唯一约束冲突。 |
| P2-10 | 已修复 | protected header 不能被 unset/set/客户端透传重新启用。 |
| P2-11 | 已修复 | 日志记录实际 execution mode，passthrough-only 禁止静默落入 transformer。 |
| P2-12 | 已修复 | Candidate Priority/Weight 进入治理排序。 |
| P2-13 | 已修复 | 健康统计按 `route_candidate_id`，并保留 degraded/stale 投影。 |
| P2-14 | 保留设计 | Canonical 治理规划后使用 Failover，保证已经排好的候选顺序不会被二次随机化。 |
| P2-15 | 已修复 | 策略排序与成本排序分离，避免哨兵/量纲覆盖协议等级。 |
| P2-16 | 已修复 | 验证会话使用明确路径或显式账号偏好，不隐式继承错误代理。 |
| P2-17 | 当前契约 | Verification binding 按 Account + ProxyConfigID + ClashNode + UA；这与现行 Site Recovery contract 一致。 |
| P2-18 | 已修复 | Credential TTL 从完成时间起算。 |
| P2-19 | 已修复 | 过期/撤销/被取代会话清除密文与待处理任务，且保留 fence。 |
| P2-20 | 已修复 | 恢复错误按结构化状态/类型分类，不再用任意数字子串判断。 |
| P2-21 | 已修复 | Verification Cookie 使用当前实际 recovery path 绑定。 |

## 补充复核

- `X-Octopus-Warning` 在每个候选前清除，failover 不残留上一候选 warning。
- Usage 下钻按请求区间内是否存在 relay log 判定，同时保留全局最早日志时间。
- `GLOBAL` Clash group 在 upsert、备份导入和运行时切换三处均拒绝。
- `VerificationSessionEnsure` 按账号使用可取消 guard，单进程并发只创建一组 session/task；多实例部署仍应配合外部协调。
- 同名 Cloudflare Cookie 保留 domain/path 作用域，按 path/domain 具体度排序；同一作用域冲突值 fail closed。
- PostgreSQL `REAL` 的金额指纹按目标存储精度规范化；MySQL `clientFoundRows=true` 场景也会在 upsert 后无条件重读并校验。
- `sitePriceFreshness=24h` 保留为当前产品策略，尚无 ADR 定义与同步间隔的推导公式。
- `CatalogSyncResult.AliasesCreated` 是保留字段；Alias 仍只允许人工创建。
- AnyRouter `acw_sc__v2` 是确定性、非交互式 JS shield cookie 计算，不是 CAPTCHA；Cloudflare/CAPTCHA 仍只走协助式人工验证。

## 验证结果

已通过定向、全包与压力测试，包括 relay terminal/cancel、usage 聚合并发、Header version、Controller fallback、WS passthrough、备份冲突回滚、迁移 021/022、并发 Verification Ensure、Cookie scope、桌面 `1440x900` 与移动 `390x844` 浏览器验收。

最终完整命令与动态验证边界记录在父任务 `completion-audit.md`。

<details>
<summary>历史原始审查报告（已失效，仅保留追溯）</summary>

以下内容是修复前的原始报告。其工具链结论、行号和开放状态均不再是当前事实。

- **审查日期**: 2026-07-29
- **审查范围**: 本轮新增的 13 篇 ADR（`docs/adr/0001~0013`）所描述的治理特性，对应提交 `cccceae`、`00ccb1b`、`961065e`、`dd4e464`（约 +5900 行）。
- **审查方式**: 需求以 `docs/adr/*.md` 为准，`CONTEXT.md` 为领域术语基线；6 个分域并行静态阅读 + 交叉复核。**7 条严重问题均已逐行读代码复核属实**。
- **验证基线**:
  - 后端:本机**无 Go 工具链、无 Docker**,无法 `go build` / `go test`。**所有后端结论均为静态阅读,未经编译与单测验证。**
  - 前端:`eslint .` ✅ 通过、`tsc --noEmit` ✅ 通过;i18n 三语言 key 完全对齐(en/zh_hans/zh_hant 各 1319 个,无缺失)。
- **行号说明**: 行号取自审查时的工作树,以文件名 + 函数名为准更稳妥。

> 标 ✅ 的条目为审查者二次读码复核确认;其余条目基于分域静态阅读,建议在有 Go 环境时补测复现。

---

## 一、总体评估

主体功能落地质量较高:13 条 ADR 的**核心语义大部分已正确实现**(见第六节覆盖统计),术语、审计字段、加密、白名单、幂等聚合等基础设施都在。**问题集中在"最后一公里"**——跨协议边界、多数据库方言(MySQL)、并发时序、安全绑定粒度、以及"规划层正确但执行层未复核"的一致性缺口。

| 严重度 | 数量 | 性质 |
|--------|------|------|
| 🔴 严重 (P0) | 7 | 需求被静默违背 / 数据不可逆丢失 / 安全越权 |
| 🟠 较高 (P1) | 20 | 功能失效 / 凭证泄露面 / 恢复闭环断裂 |
| 🟡 中 (P2) | 21 | 边界错误 / 口径不一致 / 体验缺陷 |
| ⚪ 测试缺口 | 13 | 核心行为无回归保护 |

**最需优先处理**:P0-2(MySQL 数据丢失)、P0-1(默认绕过配置代理泄露 IP)、P1 的 bridge 越权与 Gemini 密钥可覆盖——这四条要么破坏数据、要么触及安全边界。

---

## 二、🔴 严重问题 (P0)

### P0-1 ✅ 开启自动恢复后,常规同步的第一次尝试就是「直连」,绕过管理员显式配置的代理
- **位置**: `internal/sitesync/recovery.go:333`(配合 `core.go:39/97`、`recovery.go:103-164`)
- **问题**: `buildSiteRecoveryPaths` 在追加偏好路径后,**无条件把 `direct` 压入候选队列**;而账号当前显式配置的路径 `current` 只被丢进 `otherPaths` 参与打分排序。无学习偏好时,`direct` 即为第一候选。
- **触发**: 一个 `ProxyMode=pool` 且开启 `AutoProxyRecovery` 的站点,每次常规 `SyncAccount`/`CheckinAccount` 的首次尝试都直连上游,**泄露真实出口 IP**。若直连恰好成功,`learnSiteRecoveryPath` 会把偏好固化为 `direct`(recovery.go:598-608,且现有测试 `recovery_test.go:164-166` 正断言此行为),此后管理员配置的代理**永不再被使用**。
- **影响**: 直接违背 ADR-0006「实时之外的站点操作走恢复」与 ADR-0005「复用已验证路径」的意图,构成隐私/合规风险。
- **修复方向**: `current`(管理员显式配置路径)应作为**第一候选**,`direct` 仅在显式配置路径全部失败后才作为兜底;并复核 `learnSiteRecoveryPath` 不应把兜底 direct 固化为偏好。

### P0-2 ✅ MySQL 上历史修复会把 `original_error` 与 `error` 一起写成空串,原始错误不可逆丢失
- **位置**: `internal/op/log_repair.go:98-109`
- **问题**: `Updates(map[string]interface{}{"original_error": gorm.Expr("error"), "error": "", ...})`。GORM 对 map 形式 Updates 会按 key 排序生成 SET 子句,`error = ''` 排在 `original_error = error` 之前。**MySQL 单表 UPDATE 赋值自左向右求值并使用已更新的值**,于是 `original_error` 读到的是刚被清空的 `error`。
- **触发**: `OCTOPUS_DATABASE_TYPE=mysql`(go.mod 引入 `gorm.io/driver/mysql`)部署上执行任何历史修复。SQLite/PostgreSQL 用旧行值,不受影响。
- **影响**: `original_error` 与 `error` 同时清空,**原始错误文本永久丢失**,恰好摧毁 ADR-0001 要求保留的可审计证据;且不可逆。
- **修复方向**: 改用 `gorm.Expr` 一次性表达式或 `CASE`,或分两步(先备份 `original_error = error` 再清 `error`),并在 MySQL 上加回归测试。

### P0-3 ✅ 历史修复只改 `relay_logs`,不同步 usage 事实表与聚合,分析口径与日志永久矛盾
- **位置**: `internal/op/log_repair.go:95-114`(修复仅作用于 `model.RelayLog`)
- **问题**: 同一请求在 `usage_request_facts.outcome`、`usage_attempt_facts.outcome`、`usage_aggregates.failed_count` 里仍是 failed,且这些 fact 行 `aggregated_at` 已非空,**永不重新聚合**,也无任何补偿路径。
- **触发**: 任何一次历史修复之后。日志详情显示 success,但 Analytics 的成功率/失败数/趋势仍按失败计。
- **影响**: 违背 ADR-0001(修复应可信)与 ADR-0003(分析口径一致)。
- **修复方向**: 修复时在同一事务内同步更新对应 fact 行的 outcome,并将受影响聚合桶的 `aggregated_at` 置空以触发重算,或提供显式重聚合入口。

### P0-4 ✅ AnyRouter 平台的请求路径完全不携带验证会话 cookie 与 UA,人工验证闭环无法完成
- **位置**: `internal/sitesync/anyrouter.go:756-790`(`anyRouterRequestJSONWithCookies`)
- **问题**: 该函数只合并 `siteRecord.CustomHeader` 与调用方 headers,硬编码 `User-Agent`,**从不调用 `op.VerificationHeadersForAccount`**。经复核,`VerificationHeadersForAccount` 全仓唯一调用点是 `internal/sitesync/http.go:94`,AnyRouter 的 sync/checkin 走的是自己的 `anyrouter.go` 路径,不经过它。
- **触发**: AnyRouter 站点触发 Cloudflare → 创建验证任务 → 管理员完成浏览器验证 → 重试 sync/checkin → **请求仍不带 `cf_clearance`** → 再次 403 → 人工验证永远无法生效。
- **影响**: 对 AnyRouter 站点,ADR-0007/0008 的整个「协助式恢复」闭环失效。
- **修复方向**: `anyRouterRequestJSONWithCookies` 也接入 `VerificationHeadersForAccount`,或将验证头注入下沉到统一的传输层。

### P0-5 ✅ Canonical 模型策略无条件顶掉 Channel 的 `passthrough-only`
- **位置**: `internal/op/catalog.go:660-664`(`effectiveProtocolSettings`)
- **问题**: 当 `canonical.Manual || canonical.ProtocolPolicy != auto || canonical.AllowLossy` 任一成立时,用 canonical 值**整体替换**(而非取最严格)channel 的 policy/allowLossy。而 `CatalogCanonicalUpdate` 每次保存都写 `manual: true`(catalog.go:462)。
- **触发**: 管理员在模型详情页仅切换了 `enabled`,该模型 policy 即变为 canonical 默认 `auto`,渠道上配置的 `passthrough-only` **静默失效**;反向 channel 的 `allow_lossy=true` 也会被 canonical 的 `false` 抹掉。
- **影响**: 正是 ADR-0009/0012 要防止的「special upstream 被静默送进不想要的转换」。
- **修复方向**: 两级策略应取**最严格者**(passthrough-only > transform-allowed;allowLossy 取 AND),而非「后者胜」。

### P0-6 ✅ 结构化输出跨协议到 Anthropic 被静默丢弃,却判定为无损
- **位置**: `internal/op/protocol.go:324-328`
- **问题**: 非 Gemini 出站时 `structured_output` 一律返回 `FeatureCapabilityTransformed`(compatibility 保持 exact,无需 allow_lossy、无 warning)。但经复核,Anthropic 出站对 `ResponseFormat` 的唯一处理是**加一个 beta 头** `structured-outputs-2025-11-13`(`outbound/anthropic/messages.go:1635-1641`),schema 内容(`rf.Schema/RawSchema/JSONSchema`)从不写入 Anthropic 请求体。
- **触发**: `/v1/chat/completions` 带 `response_format:{type:"json_schema",...}` 路由到 Anthropic 渠道 → **JSON Schema 整个丢失**,客户端拿到自由文本,无警告无审计。
- **影响**: 违背 ADR-0012「有损转换须显式选择 + 预览 + 审计」。
- **修复方向**: 结构化输出→Anthropic 至少判 `Degraded`(需 allow_lossy + warning),或在请求体内以工具/前置提示等方式承载 schema。

### P0-7 ✅ Volcengine 渠道被误判为 `openai_chat`,产生「假 passthrough」丢弃字段
- **位置**: `internal/op/catalog.go:735-736`(`ProtocolForOutboundType` 缺 `OutboundTypeVolcengine` 分支,落入 `default: ProtocolOpenAIChat`)
- **问题**: openai_chat 入站 + Volcengine 渠道被评为 `mode=passthrough`、`compatibility=exact`、全部特性 `native`,连 `passthrough-only` 都放行。实际执行 `volcengine.ResponseOutbound.TransformRequest`(`outbound/volcengine/response.go:26-77`)会转成 Volcengine Responses 格式并显式丢弃 `Metadata` 与非白名单模型的 `Reasoning`;该适配器未实现 `PassthroughCapable`,`relay.go:959-976` 又会静默回落到全量 transform。
- **影响**: 违背 ADR-0009/0012,字段被静默丢弃且审计显示 passthrough。
- **修复方向**: 补 `ProtocolForOutboundType` 的 Volcengine 分支;并让 `AssessProtocolRoute` 依据适配器是否 `PassthroughCapable` 判定真实 mode。

---

## 三、🟠 较高问题 (P1)

### 安全 / 凭证边界
- **P1-1 ✅ Verification Bridge 三端点无鉴权,配对令牌是唯一凭据且作用域全局** — `internal/server/handlers/site_recovery.go:52-56` 只挂 `RequireJSON()` 无 `Auth()`;`VerificationTaskClaim` 取全局最旧 pending 任务不区分站点。泄露的配对令牌(默认 30 天、最长 365 天 TTL,明文存 `chrome.storage.local`)即可枚举全部待验证目标 `target_url`,并向任意账号 `/complete` 注入任意 cookie,该 cookie 随后被服务端回放到上游。无速率限制、无按账号授权。(ADR-0008)
- **P1-2 ✅ Gemini 凭证头 `x-goog-api-key` 不在保护名单,可被 Header Policy 或客户端透传覆盖** — `internal/op/header_policy.go:30-62` 保护表无 `x-goog-api-key`,前缀也不匹配。relay 顺序为「先注入凭证(`relay.go:1068`)→ 后应用策略(`relay.go:1084`)」,故:`set_headers` 可覆盖真实密钥、`unset_headers` 可删除它;允许名单配成 `*` 或 `x-goog-*` 时客户端自带 key 会覆盖渠道密钥,**Octopus 变成任意 Gemini key 的免费转发器**。(对比 Authorization/x-api-key 因在策略后注入而安全。)(ADR-0013)
- **P1-3 服务器收到的不是「最小」会话而是完整 cookie 罐** — `extensions/verification-bridge/popup.js:94` `chrome.cookies.getAll({url})` 无名称过滤,httpOnly 登录态一并上传;服务端 `verification_bridge.go:387-424` 只校验数量/长度/域名,无 `cf_clearance`/`__cf_bm` 白名单。泄露面远超 ADR-0008 所述「minimal」。(ADR-0008)
- **P1-4 加密密钥与密文同库** — `internal/op/secret.go:53-71` AES-256-GCM 密钥 = `SHA256("octopus-secret-v1\0"+JWT secret)`,JWT secret 存同库 Setting 表且无轮换入口。算法与 nonce 使用本身正确,但**拿到数据库文件即可解密全部验证 cookie**。(ADR-0007)

### 恢复闭环 / 代理偏好
- **P1-5 ✅ `SiteProxyPreferenceClearSite` 的 `Where` 缺绑定参数,站点级偏好清除完全不可用** — `internal/op/site_proxy_preference.go:221` `tx.Where("site_id = ? AND site_account_id = 0")` 只传 SQL 未传 `siteID`,参数个数校验失败、整个事务回滚。handler 恒返回 400,前端「清除站点偏好」按钮恒失败。(对比同文件账号分支写法正确)(ADR-0005)
- **P1-6 账号级 Preferred Path 覆盖无法自举,ADR-0005「账号覆盖」事实未落地** — `internal/sitesync/recovery.go:585-590` `recoveryPreferenceScopeAccountID` 只在账号**已存在**偏好时才返回 account.ID,导致学习永远只写站点级;且前端无任何设置入口(`rg preferred web/.../site/` 零命中)。(ADR-0005)
- **P1-7 账号覆盖不可用时不回退站点偏好,回退链断裂** — `internal/sitesync/recovery.go:285-290` 回退只看 `PreferredProxyConfigID==nil`;账号覆盖非空但代理被禁用/删除或全部 cooldown 时,既不用账号覆盖也不回退站点偏好,直接掉到 direct,站点学到的具体节点丢失。(ADR-0005)
- **P1-8 Clash 切换租约在切换 RPC 结束时即释放,并发恢复互相踩节点** — `internal/op/clash_controller.go:194-208` `defer` 在函数返回即释放租约,而真正的站点请求在其后才发起。任务 A 切到 node-X 释放、任务 B 立刻切到 node-Y,A 的请求实际走 node-Y → 偏好学习数据被污染;失败恢复也不还原原节点。(ADR-0004/0006)
- **P1-9 `buildSiteRecoveryPaths` 对 Clash Controller 的 HTTP 调用不受恢复预算约束、client 无超时** — `internal/sitesync/recovery.go:87` 用外层 ctx(60s 预算的 recoveryCtx 尚未创建),`newClashControllerHTTPClient` 无 `Timeout`;定时任务传 `context.Background()`,一个半开连接的控制器可让整批同步**永久阻塞**。(ADR-0006)
- **P1-10 价格同步不在恢复包装内,用原始账号代理而非恢复成功的路径** ✅ — `internal/sitesync/core.go:52` `refreshSitePricingQuotes` 在恢复返回后、用原始 `account`;账号代理失效但恢复走通另一路径时,紧接的 `/api/pricing` 仍走失效代理必然失败。(ADR-0006)

### Cloudflare 检测 / 会话绑定
- **P1-11 非 403 的 Cloudflare 拦截既不暂停重试也不建验证会话** — `internal/sitesync/http.go:166-180` `IsCloudflareProtectionResponse` 首行 `if statusCode != 403 return false`,503/429 形态的「Just a moment...」一律漏判;随后被 `siteRecoveryErrorRetryable` 判为可重试 → 在最多 3 条代理上盲目重试。(ADR-0007)
- **P1-12 HTTP 200 + 挑战页 HTML 同样不触发验证会话** — `internal/sitesync/http.go:124-127` 解析失败走 decode error,`IsCloudflareProtectionError` 为 false,操作直接失败,管理员既拿不到验证任务也看不到 `verification_required`。(ADR-0007)
- **P1-13 调用方 headers 覆盖已注入的验证 Cookie(反向亦覆盖站点自定义 Cookie)** — `internal/sitesync/http.go:94-105` 先 `Set("Cookie", 验证cookie)` 后又用 `headers` 参数 `Set` 一次。NewAPI/OneAPI 的 cookie 形态 access token 会把 `cf_clearance` 整体替换掉;反向也会覆盖 `CustomHeader` 里的 Cookie。应做 cookie 合并而非整体替换。(ADR-0007)

### 可观测性 / 聚合
- **P1-14 小时聚合被写入并按 90 天清理,但没有任何查询读取它** — `internal/op/usage_aggregate.go:148-151` 写 hour/day 两粒度,唯一读取处 `usage_analytics_aggregate.go:190-191` 硬编码 `Daily`。ADR-0003 承诺的「90 天小时聚合」只是存储开销,90 天内分析仍逐行扫 facts。(ADR-0003)
- **P1-15 `usage_hourly_retention_days` 设置对小时聚合保留完全不生效** — `internal/task/usage.go:39` 固定传 `hourlyDays=0` 回落硬编码 90;而该设置只被「分析何时切日聚合」使用。前端已把它做成可配置项,配置与实际清理点错位。(ADR-0003)
- **P1-16 `usage_request_facts`/`usage_attempt_facts` 没有任何保留策略** — `internal/op/usage_aggregate.go:458-467` 只删 hour 聚合;两张逐请求/逐尝试的事实表无删除路径,却又是 90 天内分析的唯一数据源,存储压力无上限。(ADR-0003)
- **P1-17 上游 WebSocket 拨号超时被判为客户端取消,上游失败被完全隐藏** — `internal/relay/cancel.go:48` `isClientCancellation` 只判 `DeadlineExceeded`,不区分 deadline 来自客户端还是 Octopus 给上游设的 15s 拨号超时(`ws_pool.go:404`)。结果 attempt 记为 canceled,不进 `RecordFailure`、不掉成功率——**一个持续拨不通的上游既不熔断也不掉成功率**。(ADR-0002/0001)
- **P1-18 备份恢复时聚合键冲突会静默丢失一批指标** ✅(印证) — `internal/op/backup_extended_import.go:708` `clearAggregatedAt := len(aggregates)==0`:dump 带聚合时导入的 facts 保留非空 `aggregated_at` 永不重聚合,而聚合行经 `OnConflict{DoNothing}` 导入,目标库已有同 key 则整桶丢弃。两者叠加 → 这批 token/成本/计数恢复后既不在聚合也不重算。(ADR-0003)

### 协议路由
- **P1-19 ✅ 新增分组条目在下一次 CatalogSync 之前被静默排除出路由** — `internal/op/catalog.go:583-587` 无 RouteCandidate 时 `continue`;而 `CatalogSync` 只在启动和手动接口触发,`GroupItemAdd` 不触发。给已存在 canonical 加第二个渠道后,该渠道直到重启或手动同步前**完全不参与路由**,无提示。(ADR-0010)
- **P1-20 ✅ manual 策略下 passthrough 优先排序被负载均衡器整体丢弃** — `internal/op/catalog.go:623-635` manual 分支只重排 `group.Items`,既不重写 `item.Priority` 也不设 `group.Mode`,随后 `balancer.GetBalancer(group.Mode)` 会按原始 Priority/轮转/随机再排一次。`manual` 是 UI 一等选项,选中后 ADR-0009 的「passthrough 优先」在任何分组模式下都不成立。(ADR-0009)
- **P1-21 `passthrough-only` 把同协议的 embedding/continuation/WebSocket 路由全部判死** — `internal/op/protocol.go:397-415`+`198-202` 对 embedding 出站、带 continuation/websocket 的**同协议**请求返回 `Transform`,策略闸门只看 `Mode != Passthrough` 即判 unsupported。结果:embedding 渠道设 passthrough-only → 全部 503;Responses 渠道设 passthrough-only + `previous_response_id` → 规划阶段全灭(比 HTTP replay 更早);WS 会话必然无候选。(ADR-0009/0012)

---

## 四、🟡 中等问题 (P2)

| 编号 | 领域 | 问题 | 位置 | ADR |
|------|------|------|------|-----|
| P2-1 | 定价 | `EffectivePriceForCandidate` 全局兜底只查 DB 缓存,与实际计费(models.dev)口径不一致,预览显示"未知"但账单按 global 计费 | `op/site_pricing.go:290` | 0011 |
| P2-2 | 定价 | lowest-cost 路由比价未做币种换算且忽略 `per_request`;per_request 候选 Input/Output 恒 0 → 永远被选为"最便宜" | `op/catalog.go:691-704` | 0011 |
| P2-3 | 定价 | 首次同步产生的价格行(candidate=0)永不被后续同步更新,成为不可清理的孤儿 | `sitesync/core.go:52`,`model/site_pricing.go:65-88` | 0011 |
| P2-4 | 定价 | 手动价被静默绑定到管理员未选择的 Route Candidate(`Order("id ASC").First`,不过滤 status) | `op/site_pricing.go:158-181` | 0011 |
| P2-5 | 定价 | `ValidUntil` 已过期的手动覆盖仍压过新鲜站点价(manual 分档在 fresh 判断之前) | `op/site_pricing.go:362-371` | 0011 |
| P2-6 | 定价 | PricingPanel 报价列表(按 candidate 过滤)与解析器实际生效集合(含 site 级)不一致 | `PricingPanel.tsx:84` | 0011 |
| P2-7 | Header | images/ws/compact 三条转发路径 `ResolveHeaderPolicy(ctx, channelID, 0, 0)`,**跳过 Canonical Model 与 Route Candidate 两层** | `relay/images.go:891`、`ws_pool.go:455`、`compact.go:411` | 0013 |
| P2-8 | Header | User-Agent「清空」模式实际不生效(`if policy.UserAgent != ""` 跳过空串),客户端 UA 仍透传 | `op/header_policy.go:483-485` | 0013 |
| P2-9 | Header | 编辑已有 Header Policy 带主键 id 提交,与 upsert 冲突目标(scope+scope_id)不匹配 → 保存报唯一约束错误 | `handlers/header_policy.go:45-50` | 0013 |
| P2-10 | Header | `unset_headers` 允许写入受保护头并被无条件 `Del`,一条 `["Authorization"]` 可删上游凭证 | `op/header_policy.go:177,471-475` | 0013 |
| P2-11 | 协议 | 计划的 passthrough 在执行期不复核,审计记录计划值而非实际值(日志显示 passthrough 实际转换) | `relay/relay.go:959-976` vs `301-308` | 0009/0012 |
| P2-12 | 协议 | RouteCandidate 的 Priority/Weight 前端可编辑并持久化,但排序从不读取,调了没有任何效果 | `op/catalog.go:687-719` | 0010 |
| P2-13 | 协议 | Route Candidate 健康度实际取自 Channel 级统计,同渠道不同上游模型共用;degraded/stale 永不产生 | `op/catalog.go:688` | 0010 |
| P2-14 | 协议 | canonical 解析成功后无条件 `group.Mode = Failover`,覆盖用户设的轮询/随机/加权 | `op/catalog.go:648` | — |
| P2-15 | 协议 | lowest-cost 下 `rank*1e12` 与哨兵 `-1e12` 量纲冲突,transform 可能排到 passthrough 前;float64 ULP 抹平小价差 | `op/catalog.go:616,706-709` | 0011 |
| P2-16 | 验证 | pool 模式账号手动建的验证会话绑定错代理路径(创建取 `PreferredProxyConfigID` 可能为 nil,使用取实际 ProxyConfigID),`cookie` 永不生效 | `op/verification_session.go:111-118` | 0007 |
| P2-17 | 验证 | Proxy Path 绑定丢失 ProxyMode,direct 与 system-proxy 都映射为 `ProxyConfigID=nil`,cf_clearance 张冠李戴 | `op/verification_session.go:260-280` | 0007 |
| P2-18 | 验证 | 凭据有效期按会话**创建**时刻计而非完成时刻,09:00 触发 09:14 完成 09:15 即失效 | `op/verification_session.go:51-62` | 0007 |
| P2-19 | 验证 | 过期/已完成会话的密文 cookie 从不清理,后台无清理任务,DB 无限累积 | `op/verification_session.go:363-384` | 0007 |
| P2-20 | 恢复 | `siteRecoveryErrorRetryable` 用子串匹配识别 HTTP 状态码(`Contains("500")`),`quota 500000 exceeded` 被误判可重试;与第 668 行 CF 返回 false 自相矛盾 | `sitesync/recovery.go:694-698` | — |
| P2-21 | 恢复 | Cloudflare 验证 Cookie 的节点绑定用站点偏好兜底,与当前实际路径不一致,可致 Cookie 被静默丢弃或错误附加 | `sitesync/http.go:92` | 0005 |

**另有若干低危项**:自动化反爬求解器 `anyRouterSolveAcwScV2`(阿里云盾 JS 挑战,`anyrouter.go:829-836`,与 ADR-0007「不做自动 CAPTCHA 绕过」取向冲突,建议在 ADR 显式划界或移除);`ClashControllerUpsert` 不拒绝 `GLOBAL` 分组(`clash_controller.go:66`,ADR-0004 核心诉求靠管理员自觉);`drilldown_available` 由全表 `MIN(time)` 判定与实际区间无关(`op/usage_analytics.go:213`);Header Policy 解析失败 fail-open 回退默认白名单(`relay.go:1181`);`sitePriceFreshness` 硬编码 24h 与可配置同步间隔解耦;`X-Octopus-Warning` failover 后不清除;`VerificationSessionEnsure` TOCTOU 可产生重复会话;cookie 仅按 name 去重丢失 domain/path 维度;`CatalogSyncResult.AliasesCreated` 恒为 0。

---

## 五、⚪ 测试缺口

后端整体缺少针对「跨协议损耗、多数据库方言、并发时序、安全绑定」的回归护栏,上述多条 P0/P1 正落在盲区:

1. **修复后 usage facts/aggregates 一致性无断言**(`log_repair_test.go:57-77`)→ P0-3 不可见
2. **MySQL 方言下的修复无测试** → P0-2 不可见(现有测试应仅在 SQLite 跑)
3. **Header Policy 继承链无测试**(`header_policy_test.go` 两处均 `ResolveHeaderPolicy(0,0,0)` 只覆盖 global)→ P2-7 不可见
4. **"报价绑定到别的 Route Candidate 不应命中"无用例**(fixture 只造单候选)→ 价格泄漏不可见
5. **`ApplyHeaderPolicy` 不破坏已注入上游凭证的测试只覆盖 Authorization,未覆盖 `x-goog-api-key`** → P1-2 不可见
6. **验证会话三要素绑定校验(`VerificationHeadersForAccount`)零覆盖** → P0-4、P2-16/17 不可见
7. **Bridge 端点未授权访问面无测试**(无 JWT / 错误令牌 / 已吊销) → P1-1 不可见
8. **路径构建候选顺序 + 账号→站点回退链无测试**(`recovery_test.go` 只 3 例) → P0-1、P1-7 不可见
9. **偏好清除接口无测试** → P1-5 不可见
10. **并发节点切换无测试**,也无「relay 不触发自适应切换」的回归护栏(现靠 `relay` 不 import `sitesync` 隐式保证) → P1-8
11. **两级协议策略冲突无测试**,且现有 `protocol_test.go:112-165` 把 P0-5 的错误行为固化为预期(fixture 恰好设成 transform-allowed)
12. **非 OpenAI/Anthropic 渠道类型协议判定无测试**(Volcengine/Gemini/Embedding) → P0-7、P1-21 不可见
13. **聚合保留默认路径(`hourlyDays=0`)与 per_request 计费快照链路无覆盖**

---

## 六、需求符合度(覆盖统计)

各 ADR 的**核心语义主体已正确落实**,以下为已复核确认的符合项亮点(完整清单见分域审查):

- **ADR-0001**:客户端中途断连不写渠道失败、不进熔断;取消单列不并入渠道口径;成功率分母排除 canceled;取消时仍落库已产生 usage/成本;结果按协议终态判定;修复须先预览、`confirm=true`、双审计行、`repair_batch_id` 幂等守卫。
- **ADR-0002**:每个 Client Request 只计一次(各分支 return);渠道健康按单次 Attempt;每次尝试独立落 fact + 单调 AttemptNum;按快照求和而非取最后一次。
- **ADR-0003**:原始日志沿用可配置保留(默认 7 天);日聚合长期保留;日聚合窗口内排除已聚合 facts 避免重复计数;聚合任务进程锁 + 事务幂等,有并发测试;原始过期时前端禁用下钻。
- **ADR-0004**:切换只作用于显式 `GroupName`,无 GLOBAL 隐式写入;切换前校验节点存在、切换后回读确认;控制器调用强制不走代理;secret AES-GCM 加密 + `json:"-"`;进程锁 + DB 租约双重保护。
- **ADR-0005**:站点级偏好学习已实现;sha256 身份键去重、账号级优先;成功/失败计数 + EWMA + 指数退避 + 7 天 TTL;仅网络类失败影响健康度。
- **ADR-0006**:自适应恢复严格限定 SyncAccount/CheckinAccount;**实时 relay 完全不参与**(relay 不 import sitesync);60s 预算 + 候选上限 3;站点/账号各有独立开关;每次尝试落审计。
- **ADR-0007/0008**:检测 CF 后中断重试、创建验证会话;密文存储、算法与 nonce 正确;时间受限 + 强制上限 + 过期拒用;绑定 Account/ProxyPath/UA;会话一次性(task token 用后即废、并发领取仅一成功);手动 Cookie 导入回退存在;**未引入服务端浏览器**(无 playwright/chromedp/chromium);明文 cookie 不出 API/日志/备份;扩展权限最小化并动态回收;配对令牌只存哈希、可吊销、有 TTL。
- **ADR-0009/0010/0012**:有损转换默认禁止(需 allow_lossy)、放行必带 warning;跨协议 continuation 硬性禁止;未登记特性默认 unsupported;路由预览 + 能力矩阵已落地;有损决策进入持久化审计;别名只能人工创建、canonical 名不可变;无兼容候选时明确失败不静默降级。
- **ADR-0011/0013**:价格优先级主链(manual>fresh>stale + specificity)、每请求存来源与快照、历史成本用快照;同步不覆盖手动价(identity_key 含 source);group_multiplier 只应用一次并有测试;Header 继承链 scope 顺序与 ADR 一致、后层覆盖前层;客户端透传安全白名单;保护名单覆盖 auth/proxy/cookie/host/length/forwarding/IP/hop-by-hop;CRLF 注入防护;Authorization/x-api-key 在策略后注入无法被覆盖;anthropic-beta 多值合并去重。
- **横切面**(审查者补充核查):所有新模型均纳入 `AutoMigrate`;扩展备份**导出**覆盖 canonical/alias/route_candidate/header_policy/pricing/currency/clash/preference 等新表,并剥离账号验证敏感字段;zip 导入有路径穿越防护(`Contains(name, "..")`)+ 压缩/解压大小上限 + 文件白名单;新 handler(header_policy/log_analytics/site_recovery 等)除 bridge 三端点(见 P1-1)外均挂 `Auth()`;i18n 三语言 key 完全对齐(各 1319,无缺失);`.playwright-mcp/` 已正确 gitignore。

---

## 七、修复优先级建议

**第一批(数据安全 / 安全边界,建议立即)**
1. P0-2 MySQL 修复字段序 → 不可逆数据丢失,风险最高
2. P0-1 默认绕过配置代理直连 → IP 泄露 / 合规
3. P1-1 Bridge 越权 + P1-2 Gemini 密钥可覆盖 → 认证边界
4. P0-3 修复不同步 usage → 分析口径永久失真

**第二批(需求被静默违背)**
5. P0-5/6/7 协议策略三条(canonical 顶替、结构化输出丢失、Volcengine 假 passthrough)
6. P0-4 AnyRouter 验证闭环
7. P1-5/6/7 偏好清除与账号覆盖回退链

**第三批(功能失效 / 一致性)**
8. P1-8/9 并发切换与超时;P1-11/12/13 CF 检测与 Cookie 合并
9. P1-14/15/16/18 聚合读取、保留策略、备份丢指标;P1-17 WS 拨号超时误判
10. P1-19/20/21 路由排除、manual 排序、passthrough-only 判死

**并行推进**:补齐第五节 13 项测试缺口——多条缺陷正因无回归护栏而长期不可见,补测既能复现也能防回归。

---

*本报告基于静态代码阅读。后端结论未经 `go build`/`go test` 验证(本机无 Go 环境),建议在具备 Go 工具链的环境中按第五节测试缺口补充复现用例后再行修复。*

</details>
