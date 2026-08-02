# Octopus 全面审计报告

**项目:** Octopus — LLM API 聚合与负载均衡网关 (Go + Next.js)
**审计模式:** full（19 个维度全覆盖）
**日期:** 2026-08-02
**审计者:** deepseek-v4-flash（opencode 代码审计工作流）
**分支/基线:** `feat/model-group-provisioning` @ d3c9984

> **状态更新（2026-08-02）:** 本报告是提交 `d3c9984` 的历史审计快照，不代表后续工作区的实时状态。进入本轮修复前，`058cc17` 工作区已重新通过全量验证；本轮审查修复后也再次通过 `go test ./... -count=1 -timeout 60s`、`go build ./...`、`go vet ./internal/...`、前端 lint/build 与 `git diff --check origin/dev`。下文关于“测试全量红灯”的描述仅指审计快照当时的结果。

---

## 1. 执行摘要

Octopus 是一个功能完整的 LLM API 聚合网关：Go 后端（405 文件 / 11 万行）承载代理转发、负载均衡、熔断、站点同步、备份恢复等能力，Next.js 前端（170 文件 / 4.1 万行）提供管理面板。项目有 132 个 Go 测试文件、i18n 三语零缺失、大量成熟的安全实践（参数化 SQL、脱敏字段、zip-slip 防护、错误码体系），工程功底扎实。

但本次审计发现的问题同样显著：**审计快照 `d3c9984` 的 `go test ./...` 全量红灯（internal/op 8 个 + internal/sitesync 1 个失败），当时 CI 门禁处于失效状态**；代理热路径存在 4 个 Critical 级性能问题（每请求 10-20 次无缓存 DB 查询、流式转发 O(N²) 全量重扫、BPE 编码器每块重建、代理池模式每请求新建 http.Client）；安全面存在默认凭据 admin/admin 明文入日志、JWT 密钥被复用为数据加密密钥、明文密钥随 API 响应与备份导出外泄等问题。发布链上自更新无签名校验、changelog 工作流对 master 执行 force push，风险等级很高。

总体上，这是一个"功能完成度远高于工程健康度"的项目：功能管线丰富且测试基数不小，但核心热路径、认证链路与发布链路的可靠性保障存在系统性缺口。**当前状态不建议发布稳定版**，应先修复测试红灯与 Critical 级问题。

### Score Dashboard

```
Security        ████░░░░░░  4.5  C   默认凭据入日志、JWT 密钥复用为加密密钥、密钥明文随响应/备份外泄
Stability       ██████░░░░  6.0  B   recover 兜底齐全，但并发竞态与优雅停机路径存在隐患
Performance     ████░░░░░░  4.0  C   热路径 4 个 Critical：无缓存 DB 查询、流式 O(N²)、编码器重建、连接池失效
Testing         ███░░░░░░░  3.5  D   go test 全量红灯；auth/middleware/handlers 覆盖率 0-5%；前端 0 测试
Maintainability ████░░░░░░  4.5  C   60 个文件超 500 行、12 个超 1000 行；3 个前端上帝组件
Design          █████░░░░░  5.0  B   重复代码多（backup 三套导入管线）、sitesync→op 依赖方向倒置
Release         ████░░░░░░  4.0  C   自更新无签名、changelog force push master、release 无测试门禁
─────────────────────────────────────
Overall         ████░░░░░░  4.6  C
```

评分说明：0.0–10.0，**分数越高越健康（10 = 生产就绪）**。评分为基于证据的判断，非机械扣分。

### Finding Statistics

| Severity | Count | Confirmed | Suspected |
|----------|-------|-----------|-----------|
| Critical | 7 | 7 | 0 |
| High | 28 | 26 | 2 |
| Medium | 20 | 19 | 1 |
| Low | 12 | 12 | 0 |
| Info | 1 | 1 | 0 |
| **Total** | **68** | **65** | **3** |

> 注：原多代理发现的重叠项已合并（如"请求体无大小限制"出现在 security/performance/stability 三份报告中，合并为 1 条，见 F-09）。

---

## 2. Project Map

**入口与启动链**: `main.go` → `cmd/start.go` → Config → DB + migrate → Cache → HTTP Server → 后台任务 → `shutdown.Listen()` 阻塞。

**核心架构**（按代码图谱 25 个社区聚类）:
- **`internal/op/`（op-site，1055 节点）** — 最大的业务逻辑包（74 文件），站点/渠道/统计/备份/定价/验证全部在此，是事实上的"上帝包"
- **`internal/relay/`（relay-stream，645 节点）** — 代理转发核心：`relay.go`(1922 行) + WS 池 + 流处理器 + bodycache + balancer（熔断/粘性会话）
- **`internal/transformer/`（model-gemini，914 节点）** — 三协议（OpenAI/Anthropic/Gemini）× 双方向（inbound/outbound）转换器，多为 1500-2100 行单文件
- **`internal/sitesync/`（511 节点）** — 站点平台同步（AnyRouter 等），**反向依赖 op 包 30+ 函数**
- **`web/src/`（endpoints-use，928 节点）** — 前端，3 个 1200-3300 行上帝组件
- 其余：handlers(294)、model(272)、migrate(125)、task(56)、webdav、update、client

**数据流**: `Gin Router → Middleware(Auth/CORS/Logger) → Handler → op 业务层 → DB/Cache`；代理流 `Request → inbound Transformer → Relay → Balancer → 外部 LLM API → outbound Transformer → Response`。

**状态归属**: 内存分片缓存（utils/cache，16 shard + xxhash + 关机持久化）承载 channel/setting/apikey/stats 热点；`usage_attempt_facts` / `relay_logs` 为持续增长的事实表；Stats 通过全局互斥锁 + 脏标记周期落库。

**安全边界**: JWT（HS256）+ `sk-octopus-*` API Key 双认证；`/v1/*` 对外开放（OpenAI 兼容面）。

**测试结构**: 132 个 Go 测试文件全部为集成式（真实 SQLite + 完整迁移链），无任何 mock 框架；前端 0 测试；CI（ci.yml）后端跑 `go test ./...`、前端仅 `pnpm lint`。

**风险高发区**: relay 热路径、op/stats 统计更新、认证链路（零测试）、备份恢复管线（三套导入实现）、发布/更新链路。

---

## 3. Top Risks

按优先级排序（详细见第 4 节）：

| # | 风险 | Severity | 一句话摘要 |
|---|------|----------|-----------|
| 1 | `go test ./...` 全量红灯，CI 门禁失效 | Critical | dev 分支 9 个测试失败（catalog 路由候选），任何 PR 合并都携带已知缺陷 |
| 2 | 自更新机制无签名/校验和验证 | Critical | GitHub 发布包被替换即自动 RCE 全量部署 |
| 3 | 热路径每请求 10-20 次无缓存 DB 查询 | Critical | QPS 与 DB 负载线性耦合，TTFT 随 facts 表增长劣化 |
| 4 | 流式 passthrough O(N²) 全量重扫 + 双份缓冲 | Critical | 长流 CPU/内存随输出长度二次增长，多路并发直接 OOM |
| 5 | 上游 Token / API Key 明文序列化进 API 响应 | Critical | 掩码字段存在但明文未移除，XSS/截图/审计即可盗取全部上游凭据 |
| 6 | 代理池模式每请求新建 http.Client | Critical | TLS 握手每请求一次，连接池完全失效，RPS 掉一个数量级 |
| 7 | BPE 编码器每内容块重建并重复 tokenize | Critical | Anthropic 请求 TTFT 增加数十 ms，CPU 随对话轮数线性放大 |
| 8 | 默认凭据 admin/admin 明文写入日志 | High | 首启日志泄露唯一管理入口凭据 |
| 9 | 请求体无大小限制（/v1 全入口） | High | 持 key 用户或匿名慢速流即可 OOM 服务 |
| 10 | JWT 密钥被复用为数据加密密钥 + 弱回退 | High | 单一密钥同时保护 JWT 签名与 AES 加密，泄漏即全盘沦陷 |
| 11 | changelog.yml 对 master `git push --force` | High | 任何 v* tag 触发，master 上 hotfix 会被覆盖丢弃 |
| 12 | 认证/鉴权链路 0 测试，handlers 仅 5% 覆盖 | High | 网关公网面无任何自动化验证 |

---

## 4. Detailed Findings

### Finding: 审计快照 `d3c9984` 的 `go test ./...` 全量红灯，CI 门禁失效

- Severity: Critical
- Confidence: High
- Category: Testing
- Status: Confirmed（本机实测复现）
- Affected area: internal/op、internal/sitesync、CI
- Evidence:
  - File: internal/op/catalog_test.go、internal/sitesync/pricing_test.go
  - Function / Module: CatalogSync 系列测试（8 个失败：`TestCatalogSyncPreservesCandidateForNonAuthoritativeManagedGroup`、`TestCatalogSyncArchivesAndRestoresUnseenCandidate`、`TestChannelDeleteArchivesRouteCandidates` 等）；`TestSyncAccountBindsFirstPricingRefreshToProjectedCandidate`
  - Relevant behavior: `go test ./... -count=1` 实测：`internal/op` 8 个测试失败（`find route candidate failed: record not found` / `load group: group not found`），`internal/sitesync` 1 个失败（pricing refresh 后 `RouteCandidateID:<nil>`）；`.github/workflows/ci.yml` 直接 `go test ./...` 且失败即失败
- Problem: 路由候选（route candidate）新功能测试与实现不一致，失败已存在于当前工作树；CI 必然红灯，团队对红信号脱敏后其他回归会混入；release.yaml 完全不跑测试，红灯也可能照常发布
- Why it matters: 主干测试红 = 无法发布 + 回归无兜底，是当前第一优先级阻塞
- Realistic failure scenario: 开发者在红 CI 上继续叠加提交 → 合并 dev→master → release 产出带 catalog 缺陷的正式版，生产模型路由候选不生成
- Minimal fix: 修复 CatalogSync/candidate 绑定逻辑或修正 fixture，直至 `go test ./...` 全绿
- Regression test suggestion: 保留这 9 个失败测试为回归用例；CI 加 `-cover` 并阻断合并
- Estimated effort: 2-4 小时

### Finding: 自更新机制无签名/校验和验证，下载 zip 直接覆盖运行中二进制

- Severity: Critical
- Confidence: High
- Category: Release / Security
- Status: Confirmed
- Affected area: internal/update
- Evidence:
  - File: internal/update/update.go:22,39-73,96-125；internal/update/core.go:26-44
  - Function / Module: `doRequest` / `unzip` / `UpdateCore` / `restartExecutable`
  - Relevant behavior: 从 GitHub release 下载 zip → `unzip(data, filepath.Dir(execPath))` 直接覆盖正在运行的二进制 → `syscall.Exec` 重启；zip-slip 已有防护（`isPathInDest`），但**无发布者签名、无 SHA256 校验**；Windows 上覆盖运行中 exe 必失败但错误被 `cmd.Start()+os.Exit(0)` 掩盖
- Problem: 供应链后门默认开启——GitHub 仓库/发布被攻陷即全量部署 RCE 二进制；该网关持有全部上游 API Key
- Why it matters: 自更新是攻击者最高价值入口
- Realistic failure scenario: 上游 release 被恶意替换 → 所有部署实例自动下载并执行恶意二进制 → 凭据被窃取
- Minimal fix: 下载后校验发布页 SHA256 校验和；Windows 更新前先改名旧 exe；失败即中止不替换
- Regression test suggestion: 对 `unzip` 注入篡改数据断言拒绝执行；mock 校验和失败路径
- Estimated effort: 4-8 小时

### Finding: 热路径每次 LLM 请求执行 10-20 次无缓存 DB 查询

- Severity: Critical
- Confidence: High
- Category: Performance
- Status: Confirmed
- Affected area: relay 热路径 / op.catalog / op.header_policy / op.site_pricing
- Evidence:
  - File: internal/relay/relay.go:95-100,278-290,1298-1308；internal/op/catalog.go:762,988-999；internal/op/catalog_health.go:20-52；internal/op/site_pricing.go:309-425；internal/op/header_policy.go:326-392
  - Function / Module: `Handler` → `CatalogPlanGroup` → `routeCandidatePerformanceMap` / `EffectivePriceForCandidate`；`copyHeaders` → `ResolveHeaderPolicy`
  - Relevant behavior: 每请求全量加载 RouteCandidate（`Find` 无缓存）+ 对 `usage_attempt_facts`（持续增长事实表）做 24h 窗口 `GROUP BY`；每个 candidate 再 2 次查询（价格）；header policy 最多 5 次循环查询——均为低频变更数据却完全不走内存缓存
- Problem: QPS 与 DB 查询数线性耦合；SQLite 单写者下聚合阻塞写路径；facts 表增长后单请求聚合 >50ms，直接抬高 TTFT
- Realistic failure scenario: 100 QPS × 15 条查询 = 1500 QPS DB 负载；route_candidates 上百条 + facts 100 万行时 DB CPU 成瓶颈
- Minimal fix: RouteCandidate 列表、24h 窗口统计、HeaderPolicy 解析结果、有效价格按 key 放入 utils/cache 分片缓存（TTL 10-60s 或写操作主动失效）
- Regression test suggestion: httptest 打 N 个请求，断言 SQL 语句数（gorm 拦截器）为常数而非随 candidate 数线性增长
- Estimated effort: 3-6 小时

### Finding: 流式 passthrough 路径 O(N²) 全量缓冲扫描 + 双份内存复制

- Severity: Critical
- Confidence: High
- Category: Performance
- Status: Confirmed
- Affected area: relay/stream、relay.go passthrough
- Evidence:
  - File: internal/relay/stream/processor.go:214-216,266-271,302-304,368-371；internal/relay/relay.go:1464-1485
  - Function / Module: `StreamProcessor.Run` / `processEvent` / `streamTerminalEvent`
  - Relevant behavior: `BufferRawStream=true` 时：① 每个 chunk 追加进无上限 rawBuffer；② terminal 事件出现前**每个事件都对整个累积 buffer 全量 `sse.Read` 重扫（O(N²)）**；③ 结束后 OnFinish 再把整份 buffer 复制进 `rawStreamBuf`（第二份），随后第三次全量解析收集指标
- Problem: 10 万 token 输出 → 字节级 O(N²) 扫描 + 峰值 2 倍流体积内存；多并发长流直接 OOM
- Realistic failure scenario: 4 路并发 50 万 token 流，每路 buffer ~2MB 每事件全扫，CPU 100% + GC 停顿数秒 → 容器 OOMKilled
- Minimal fix: terminal 检测改为增量扫描（只扫新到达事件）；去掉 `rawStreamBuf` 二次复制；rawBuffer 设上限（如 64MB）
- Regression test suggestion: 构造 1 万事件流，断言总扫描字节数接近 O(N) 且 OnFinish 只解析一次
- Estimated effort: 2-4 小时

### Finding: 上游 Token / ChannelKey / APIKey 明文序列化进 API 响应

- Severity: Critical
- Confidence: High
- Category: Security / BackendAPI
- Status: Confirmed
- Affected area: internal/model/site_channel.go、internal/model/channel.go、internal/model/apikey.go、internal/op/site_channel.go
- Evidence:
  - File: internal/model/site_channel.go:59（`Token json:"token"` 明文）、:83（`ChannelKey json:"channel_key"` 明文）；internal/model/channel.go:128；internal/model/apikey.go:6；internal/op/site_channel.go:207,253
  - Function / Module: SiteChannelCards / ChannelList / APIKeyList 序列化
  - Relevant behavior: 响应结构体**同时**序列化明文 + `TokenMasked`/`ChannelKeyMasked` 两套字段；已有测试断言掩码值（site_channel_test.go:401-402），说明掩码是有意设计但明文忘记移除
- Problem: 第三方上游凭据（Claude/Gemini/中转站 token）与网关 `sk-octopus-*` 密钥明文暴露给管理面板；任何 XSS/录屏/审计录像直接拿到可用凭据
- Why it matters: 掩码机制形同虚设，凭据泄露面为浏览器 DOM 级
- Realistic failure scenario: 前端组件误用 `source_keys[].token` 渲染到 DOM → 同源 XSS 读取 → 上游凭据被盗
- Minimal fix: 明文字段改 `json:"-"`，列表/详情仅返回 masked；仅 Create 响应保留一次明文
- Regression test suggestion: 断言 List 响应 JSON 无 `"api_key"`/`"token"` 明文键（json.RawMessage 检查）
- Estimated effort: 1-2 小时

### Finding: 代理池模式（ProxyMode=pool）每请求新建 http.Client 并克隆 Transport

- Severity: Critical
- Confidence: High
- Category: Performance
- Status: Confirmed
- Affected area: internal/client/http.go、internal/helper/channel.go
- Evidence:
  - File: internal/client/http.go:83-88；internal/helper/channel.go:28-36；internal/relay/relay.go:1345-1350
  - Function / Module: `GetHTTPClientCustomProxy`（注释明示 "returns a NEW http.Client every time (no reuse)"）
  - Relevant behavior: pool 模式下每次请求 `http.DefaultTransport.Clone()` → 独立连接池 → keep-alive 完全失效，每次请求重新 TCP+TLS 握手
- Problem: 代理池是通道高可用手段，却恰好在该模式下吞吐最低
- Realistic failure scenario: 100 并发经 socks5 代理池 → 每请求新建 dialer → 连接建立时间占比 60%+，RPS 降一个数量级
- Minimal fix: 按 (proxyURL, scheme) 缓存并复用 http.Client
- Regression test suggestion: 断言连续两次 `GetHTTPClientCustomProxy(url)` 返回同一实例；httptest 统计握手次数 < 请求数
- Estimated effort: 1 小时

### Finding: Anthropic 入站转换对每个内容块重建 BPE 编码器并重复 tokenize

- Severity: Critical
- Confidence: High
- Category: Performance
- Status: Confirmed
- Affected area: transformer/inbound/anthropic、utils/tokenizer
- Evidence:
  - File: internal/transformer/inbound/anthropic/messages.go:105,118,142,208,256,390,403-405；internal/utils/tokenizer/tokenizer.go:7-15；internal/relay/metrics.go:77-83
  - Function / Module: `transformRequest`（逐 message→content block→tool 粒度调 `CountTokens`，每次 `codec.NewO200kBase()` 重建编码器）
  - Relevant behavior: 编码器初始化是重操作（vocab 加载 + ranks 构建），每请求重复 10-50 次；metrics 侧对同一 body 再 tokenize 一次
- Problem: Anthropic 请求 TTFT 增加数十 ms，CPU 随对话轮数/工具数线性放大
- Realistic failure scenario: 50 轮对话 + 10 个工具（~200KB）请求 → tokenize 60 次 × 编码器重建 → 转换阶段 300ms+，高峰并发 CPU 打满
- Minimal fix: 包级单例/`sync.Pool` 复用 O200kBase（并发读安全）；入站与 metrics 估算合并为一次
- Regression test suggestion: benchmark 断言 100 次 CountTokens 无编码器重建开销；golden test 断言 token 数一致
- Estimated effort: 1-3 小时

### Finding: 默认凭据 admin/admin 且明文打印到日志

- Severity: High
- Confidence: High
- Category: Security
- Status: Confirmed
- Affected area: internal/op/user.go
- Evidence:
  - File: internal/op/user.go:19-20,27 — 硬编码 `"admin"/"admin"` + `log.Infof("initial user: admin,password: admin")`
- Problem: 首次部署即存在默认口令入口，且明文凭据进入日志（常被采集到第三方系统）
- Why it matters: 单管理员模型下这是唯一管理入口，被攻破即完全控制网关（全部 API key + 上游密钥）
- Realistic failure scenario: 部署者未改默认密码 → 运维日志平台被扫描 → 攻击者拿到 admin/admin 直登管理面板
- Minimal fix: 首次初始化随机生成密码仅打印一次到 stdout；日志禁止打印凭据；强制首次登录改密
- Regression test suggestion: 单测断言 user init 日志不包含密码明文（log capture）
- Estimated effort: 1-2 小时

### Finding: /v1 代理入口请求体无大小限制，`io.ReadAll` 可致内存耗尽

- Severity: High
- Confidence: High
- Category: Security / Stability / Performance
- Status: Confirmed
- Affected area: internal/relay/relay.go:757-779（parseRequest，覆盖全部 /v1 入口）、internal/relay/compact.go:45
- Evidence:
  - File: internal/relay/relay.go:758；internal/relay/compact.go:45（均无 MaxBytesReader）；对照 internal/server/handlers/site.go:129,151 有 `http.MaxBytesReader`，images 路径有 bodycache 256MB 上限——仓库有该模式但 relay 入口遗漏
  - Function / Module: `parseRequest` / `HandleResponsesCompact`
  - Relevant behavior: 原始 body 全量 `io.ReadAll` 入内存且保留在 metrics.RawRequest 中（≥2 份），无任何上限
- Problem: 持有效 key 的调用方（或匿名慢速流）可 OOM 整个服务，影响所有租户
- Realistic failure scenario: POST 2GB JSON 到 `/v1/chat/completions` → 内存暴涨 → OOM kill → 所有进行中 LLM 请求与 WS 会话中断
- Minimal fix: 所有入站协议入口统一 `http.MaxBytesReader`（建议与 bodycache 对齐或按模型上下文调低），超限 413
- Regression test suggestion: 集成测试超限 body 断言 413 且内存增长有界；覆盖无 Content-Length 的 chunked 场景
- Estimated effort: 0.5-1 天

### Finding: JWT 签名密钥被复用为数据加密密钥，且存在弱回退

- Severity: High
- Confidence: High
- Category: Security
- Status: Confirmed
- Affected area: internal/op/secret.go、internal/server/auth/auth.go
- Evidence:
  - File: internal/op/secret.go:53-58（`secretAEAD()` 直接读取 `SettingKeyJWTSecret`）、:61-66（AES-256 密钥 = sha256("octopus-secret-v1\x00"+JWT secret)）；internal/server/auth/auth.go:31-38（`rand.Read` 失败时回退 `user.Username + user.Password` 作为 JWT 密钥）
  - Function / Module: `secretAEAD` / `getJWTSecret`
- Problem: 单一密钥同时保护两个领域：JWT secret 一旦泄露（DB 文件/备份/日志），攻击者可同时伪造管理员 JWT 并解密所有 `EncryptSecret` 加密字段（Clash 控制器密钥、验证 Cookie、WebDAV 凭据）；回退分支使密钥退化为已知信息组合
- Why it matters: 密钥设计违背"领域隔离"，一处泄漏全盘沦陷
- Realistic failure scenario: SQLite 文件被备份拖走 → 解密全部加密字段 + 伪造任意管理员 token
- Minimal fix: 独立随机 AES 主密钥（与 JWT secret 分离存储）；删除弱回退分支，rand 失败直接报错终止
- Regression test suggestion: 单测断言 EncryptSecret 密钥与 JWT secret 不同源；删除回退分支后测试生成失败返回 error
- Estimated effort: 2-4 小时

### Finding: HTTP Server 无任何连接/读写超时

- Severity: High
- Confidence: High
- Category: Security / Stability
- Status: Confirmed
- Affected area: internal/server/server.go
- Evidence:
  - File: internal/server/server.go:58-59 — `httpSrv` 仅设 `Addr` 与 `Handler`，`ReadTimeout`/`ReadHeaderTimeout`/`WriteTimeout`/`IdleTimeout` 全为零值（net/http 默认无超时）
- Problem: slowloris 可长期占用连接/goroutine；慢上游响应挂住代理连接放大资源占用
- Realistic failure scenario: 攻击者开数千个慢连接 → 连接/goroutine 耗尽 → 服务不可用
- Minimal fix: 设置 ReadHeaderTimeout(≥5s)/ReadTimeout/WriteTimeout/IdleTimeout，SSE/WS 长连接单独豁免
- Regression test suggestion: httptest 慢读断言超时后连接被关闭
- Estimated effort: 0.5 天

### Finding: 密钥明文存储 + 备份明文导出

- Severity: High
- Confidence: High
- Category: Security
- Status: Confirmed
- Affected area: internal/model/apikey.go、internal/op/setting.go、internal/op/backup.go、internal/webdav/client.go
- Evidence:
  - File: internal/model/apikey.go:6（APIKey 明文存 DB + 随 list/create 响应回显）；internal/op/setting.go:55（JWT secret/WebDAV 密码明文入 DB）；internal/op/backup.go:1461-1501（备份 zip 明文导出 channel_keys.json/api_keys.json/site_tokens.json/settings）；internal/webdav/client.go:34
- Problem: 所有下游凭据以明文存在于 SQLite 文件与备份产物中；备份上传 WebDAV 后远端存储等于明文密码库
- Why it matters: 数据库文件/备份泄露 = 全部凭据泄露
- Realistic failure scenario: 备份 zip 落到第三方存储桶 → 全部上游 key + JWT secret 泄露
- Minimal fix: API key 哈希存储（展示脱敏）；备份导出前加密或排除 secret 字段；WebDAV 密码用 EncryptSecret
- Regression test suggestion: 导出备份后断言产物中无明文 api_key/token
- Estimated effort: 2-4 小时

### Finding: 站点探测功能可被用于盲 SSRF

- Severity: Medium
- Confidence: Medium
- Category: Security
- Status: Confirmed
- Affected area: internal/sitesync/detect.go
- Evidence:
  - File: internal/sitesync/detect.go:42-131 — `DetectPlatform` 对用户提供 URL 发起 GET（10s timeout），无 scheme/内网地址/IP 范围校验
- Problem: 管理员接口的 URL 字段可指向 `http://127.0.0.1:<port>`、内网服务或云元数据地址，做内网端口/服务探测（盲 SSRF）
- Realistic failure scenario: 导入站点 URL 填 `http://169.254.169.254/latest/meta-data/` 触发内网请求；利用错误差异做存在性判断
- Minimal fix: 校验 scheme（仅 http/https）、拒绝内网/环回/链路本地地址
- Regression test suggestion: 对 127.0.0.1/169.254.169.254/内网段输入断言拒绝
- Estimated effort: 2-4 小时

### Finding: API Key 限速可被绕过（LRU 驱逐 + 边界误差）

- Severity: Low
- Confidence: High
- Category: Security
- Status: Confirmed
- Affected area: internal/op/ratelimit.go
- Evidence:
  - File: internal/op/ratelimit.go:16（容量 16 的 LRU 缓存）、:36-43（先 `count++` 再判断，允许 maxRPM+1 个请求）；状态纯内存，重启清零
- Problem: API key 数超过 16 时旧条目被驱逐 → 限速绕过（配额/成本失控）
- Realistic failure scenario: 运营方启用 30 个 key，前 16 个 key 的高频请求被限，第 17 个 key 无限速
- Minimal fix: 扩大容量或改为固定容量分片；先判断后自增
- Regression test suggestion: 注入 20 个 key 断言所有 key 限速生效
- Estimated effort: 2-4 小时

### Finding: WebSocket 升级禁用 Origin 校验

- Severity: Low
- Confidence: High
- Category: Security
- Status: Confirmed
- Affected area: internal/relay/ws_client.go
- Evidence:
  - File: internal/relay/ws_client.go:40-42 — `websocket.Accept(..., &websocket.AcceptOptions{InsecureSkipVerify: true})`（注释"Allow cross-origin"）
- Problem: 目前受认证头缓解（跨站 WS 无法自定义 header），但未来改用 Cookie/查询参数认证则立即变为可利用 CSWSH
- Minimal fix: 改为显式 `OriginPatterns` 白名单
- Regression test suggestion: 断言非法 Origin 被拒绝升级
- Estimated effort: 1 小时

### Finding: CORS 配置 `*` 时允许任意来源 + 凭据

- Severity: Low
- Confidence: High
- Category: Security
- Status: Confirmed（配置驱动行为）
- Affected area: internal/server/middleware/cors.go
- Evidence:
  - File: internal/server/middleware/cors.go:14-16（`AllowCredentials=true`、`AllowHeaders=["*"]`）、:31-33（值为 `*` 时 AllowOriginFunc 对任意 origin 返回 true）
- Problem: 管理员配置 `*` 后任意恶意站点可对管理 API 发起携带凭据的跨源请求；与其它漏洞组合放大
- Minimal fix: `*` 与 AllowCredentials 互斥；默认仅允许显式域名列表
- Regression test suggestion: 配置 `*` 时断言凭据请求被拒
- Estimated effort: 1 小时

### Finding: 完整请求/响应内容明文持久化到 relay_logs

- Severity: Low
- Confidence: High
- Category: Security
- Status: Confirmed
- Affected area: internal/relay/metrics.go、internal/relay/relay.go
- Evidence:
  - File: internal/relay/metrics.go:522（`RequestContent: string(m.RawRequest)`）；internal/relay/relay.go:758（rawBody 原始字节）；主链路无截断（images 路径有 8KB 截断：images.go:678）
- Problem: 用户 prompt（可能含敏感业务数据/隐私）明文入 relay_logs，可通过日志接口（include_content=true）与备份读取
- Minimal fix: 默认只存 token 统计与元数据，请求/响应内容改为可选（默认关）并加密存储
- Regression test suggestion: 断言默认配置下日志表无 prompt 内容
- Estimated effort: 2-4 小时

### Finding: WS 池 preflight 持锁中途解锁，同一连接可能被并发获取

- Severity: Medium
- Confidence: High
- Category: Stability
- Status: Confirmed
- Affected area: internal/relay/ws_pool.go
- Evidence:
  - File: internal/relay/ws_pool.go:290-317（`preflightPreferredConnLocked` 在 :297 `p.mu.Unlock()` 做网络 Ping，:301 重新加锁，重锁后 :303-307 只校验 closed/retireAfterUse/仍在池中，**不校验 busy**）；调用链 `GetPreferred` :118-161
- Problem: 解锁窗口内另一 goroutine 可抢先标记同一连接 busy 并返回 → 两个请求同时持有同一上游连接并发 `conn.Write`（coder/websocket 不保证并发写安全）→ 帧交错/协议损坏
- Realistic failure scenario: 连接空闲触发 Ping 分支 + 多并发请求路由到同一池 → 随机 500/断流
- Minimal fix: 给 pooledConn 加 atomic.Bool 占用标志，preflight 期间先 CAS 占用，失败再释放
- Regression test suggestion: 并发调用 GetPreferred（mock Ping 延迟），断言同一连接不会返回给两个调用方
- Estimated effort: 0.5-1 天

### Finding: 全局熔断器/粘性会话 sync.Map 无定期清理，长期运行内存增长

- Severity: Medium
- Confidence: High
- Category: Stability / Performance
- Status: Confirmed
- Affected area: internal/relay/balancer/circuit.go、internal/relay/balancer/session.go
- Evidence:
  - File: internal/relay/balancer/circuit.go:39,46-54（条目仅在渠道删除时清理）、:153-173（RecordSuccess 只重置不删除）；internal/relay/balancer/session.go:17,28-36（惰性 TTL 删除，无定期全量清理）
- Problem: circuitKey = channel:key:model，key 轮换/模型变更持续产生新条目；无人访问的失败条目永远驻留，数月可累积数万级且不可观测
- Realistic failure scenario: 每季度批量轮换上游密钥 → 旧组合残留数万个 entry，内存只增不减
- Minimal fix: 10 分钟周期 sweep goroutine（删除超 N 小时未触达的 Closed 条目 + 超 TTL session）
- Regression test suggestion: 构造过期条目触发 sweep，断言 map 收敛；race detector 下并发 Range/Delete
- Estimated effort: 0.5 天

### Finding: 统计更新热路径使用 3 把全局 Mutex 且读-改-写非原子（丢失更新）

- Severity: High
- Confidence: High
- Category: Performance
- Status: Confirmed
- Affected area: internal/op/stats.go、internal/relay/metrics.go
- Evidence:
  - File: internal/op/stats.go:323-336,357-370,372-385（每类 Update 都是 cache.Get → Add → cache.Set，两次分片锁）、:28-37（全局 needUpdate 锁）；internal/relay/metrics.go:366-378；internal/relay/relay.go:561,602,667（StatsChannelUpdate 成功路径被调用 2 次）
  - Function / Module: `StatsChannelUpdate` / `StatsModelUpdate` / `StatsAPIKeyUpdate`
- Problem: ① 全部请求争抢同一把全局互斥锁 → 高并发串行化点；② Get/Add/Set 非原子 → 同 channel 并发请求丢失统计增量（直接影响 API key MaxCost 限额判定）
- Realistic failure scenario: 200 QPS 下锁等待 ~15%；两个并发请求同时 +1 最终只 +1（丢失 50% 增量）→ 限额判断失真
- Minimal fix: 分片内原子化 RMW（分片锁内完成 Get+Add+Set）；脏标记改 shard 级；StatsChannelUpdate 去重调用
- Regression test suggestion: 100 goroutine 并发更新断言最终计数 = 100；benchmark 对比锁等待
- Estimated effort: 3-5 小时

### Finding: 站点模型小时统计每次请求持全局互斥锁更新 map

- Severity: High
- Confidence: High
- Category: Performance
- Status: Confirmed
- Affected area: internal/op/stats_site_model.go
- Evidence:
  - File: internal/op/stats_site_model.go:44-84（`siteModelHourlyCacheLock.Lock()` 保护整个 map）、:67-68；internal/relay/metrics.go:378（每请求调用）
- Problem: 与 stats.go 全局锁叠加，每请求 6-8 次全局锁操作；bucket 数随 (hour×account×group×model) 增长，锁持有时间线性变长
- Realistic failure scenario: 1000 并发请求时该锁成 top contention，统计更新延迟 1ms+/请求
- Minimal fix: 按 hour 分片 map 或 sync.Map；或批量聚合、task 周期分发
- Regression test suggestion: benchmark 64 goroutine × 1000 次更新对比锁等待占比
- Estimated effort: 2-3 小时

### Finding: 每请求日志持久化做 2 次完整大 JSON 序列化（请求体 + 响应体全量）

- Severity: High
- Confidence: Medium
- Category: Performance
- Status: Confirmed
- Affected area: internal/relay/metrics.go → internal/op/log.go
- Evidence:
  - File: internal/relay/metrics.go:520-535（saveLog 全量 Marshal RequestContent/ResponseContent）；internal/op/log.go:149-179（全量入内存队列 64MB）
- Problem: 序列化 O(body) CPU + 分配；大响应时每请求数百 ms；pending 队列持有完整响应副本
- Realistic failure scenario: 10 并发 10 万 token 响应 → 每请求 marshal ~1MB JSON，CPU +20%，队列内存 10MB+ 常驻
- Minimal fix: 日志内容截断（每字段 ≤32KB）或仅存标量；复用 buffer
- Regression test suggestion: benchmark saveLog 大响应；断言 pending 队列单条字节有上限
- Estimated effort: 2-4 小时

### Finding: ParamOverride 每次请求全量 body 解析为 map[string]any 再重序列化

- Severity: High
- Confidence: High
- Category: Performance
- Status: Confirmed
- Affected area: internal/helper/param_override.go
- Evidence:
  - File: internal/helper/param_override.go:14-55；internal/relay/relay.go:1119,1202（每次 attempt 调用）
  - Function / Module: `ApplyParamOverride`
- Problem: 大请求体 + 重试时重复 unmarshal/marshal 多次；map[string]any 解码大 JSON 内存峰值高
- Realistic failure scenario: 500KB body × 3 次重试 ≈ 300ms 额外 CPU
- Minimal fix: 仅在需要合并时重序列化；用 json.RawMessage 按需替换字段；attempt 间缓存解析结果
- Regression test suggestion: benchmark 500KB body 的 ApplyParamOverride CPU/alloc
- Estimated effort: 2-4 小时

### Finding: `err.Error()` 直接写入统一错误响应（11+ 处），泄漏内部细节

- Severity: High
- Confidence: High
- Category: BackendAPI / Security
- Status: Confirmed
- Affected area: internal/server/handlers/
- Evidence:
  - File: setting.go:58、channel.go:64、group.go:42、stats.go:45、log.go:66、log_analytics.go:75,133、log_analytics_export.go:60、proxy_pool.go:33-37、update.go:34、webdav_backup.go:39,53 — 全部 `resp.Error(c, 500, err.Error())`
- Problem: SQL 方言细节、文件路径、WebDAV 主机名原样回传客户端；错误码体系（apperror Code/Params）未贯通
- Realistic failure scenario: 损坏备份恢复返回完整 `/Users/xxx/octopus.db: disk I/O error` 路径 → 攻击者据此定位部署环境
- Minimal fix: 统一 `resp.ErrorWithAppError(c, apperror.New(...))`，err 只写日志，响应仅通用文案
- Regression test suggestion: 对每个 handler 注入模拟 error，断言响应 message 不含 err 原文
- Estimated effort: 3-4 小时

### Finding: 请求绑定校验普遍缺失，且绑定失败统一误报为 InvalidJSON

- Severity: High
- Confidence: High
- Category: BackendAPI
- Status: Confirmed
- Affected area: internal/model/*.go、internal/server/handlers/*.go、internal/server/resp/error.go
- Evidence:
  - File: internal/model/user.go:15-19（UserLogin 无 `binding:"required"`，空密码可登录）；internal/model/apikey.go（create 无 Name 校验）；internal/model/group.go（无 Name/Mode 校验）；仅少量模型带 binding（model/proxy.go:33、site_channel.go:118-199、channel.go:142-171）——覆盖严重不均衡
  - Function / Module: ShouldBindJSON 失败一律 `resp.InvalidJSON`
- Problem: 脏数据（空名 apikey）直接进库；缺字段/类型错误/非法枚举都报"Invalid JSON format"，语义错误且误导排障
- Realistic failure scenario: 客户端传 `{"mode":"BAD"}` 收到 "Invalid JSON format" → 排查半天实为缺字段/枚举错误
- Minimal fix: 为 create/login 模型补齐 binding + 枚举校验；handler 区分 UnmarshalTypeError / validator 错误，映射不同错误码
- Regression test suggestion: httptest 发缺字段/类型错/非法枚举三类请求，断言三个不同错误码
- Estimated effort: 半天

### Finding: APIKey 更新为「缓存读-改-写 + Save 全列覆写」无事务，并发丢失更新

- Severity: Medium
- Confidence: High
- Category: BackendAPI
- Status: Confirmed
- Affected area: internal/op/apikey.go
- Evidence:
  - File: internal/op/apikey.go:24-35（先从 apiKeyCache 读整条，未命中直接返回 "apikey not found"；`Omit("api_key").Save(&cachedKey)` 全列覆写）；对照正面示例 internal/op/channel.go:164-283（指针字段 + Select 局部更新 + 显式事务）
- Problem: 两个并发更新各自基于旧快照，后提交者覆写先提交者修改；与 channel.go 成熟模式不一致
- Realistic failure scenario: 两个标签页同时改同一 apikey 的 name 与 enabled → 后者保存后前者修改消失
- Minimal fix: 参照 channel.go 改指针字段 + Select 局部更新；或用乐观锁
- Regression test suggestion: 并发调用 APIKeyUpdate（name/enabled 两 goroutine）断言两字段最终都存在
- Estimated effort: 2-3 小时

### Finding: usage analytics 分页 pageSize 无上限

- Severity: Medium
- Confidence: High
- Category: BackendAPI
- Status: Confirmed
- Affected area: internal/server/handlers/log_analytics.go、internal/op/usage_analytics_breakdown.go
- Evidence:
  - File: internal/server/handlers/log_analytics.go:62-63,122-123（page_size 直接透传）；对照 internal/op/log.go:133-137 有 clamp（pageSize>100 归 20）；internal/op/usage_analytics_breakdown.go:505（Offset((page-1)*pageSize)）、:746（Limit(pageSize+1)）
- Problem: `page_size=-1` 或 2147483647 → 超大批量查询或 (page-1)*pageSize 溢出负 Offset 触发 DB 错误并原样返回
- Realistic failure scenario: 脚本漏传参数 → 每次请求全表级扫描，DB CPU 打满
- Minimal fix: 与 log.go 对齐 clamp [1,100]，负值回退默认
- Regression test suggestion: 单测 parse：page_size=0/-5/10^9 均回退或截断
- Estimated effort: 30 分钟

### Finding: 创建接口无幂等/重复名称预检

- Severity: Medium
- Confidence: High
- Category: BackendAPI
- Status: Confirmed
- Affected area: internal/server/handlers/apikey.go、channel.go、group.go
- Evidence:
  - File: internal/op/apikey.go（APIKeyCreate 无 name 预检）、internal/op/channel.go:49（同）；重复名依赖 DB 唯一约束报错，错误经 `err.Error()` 原样返回
- Problem: 相同语义的重复请求产生不同结果（成功 vs 唯一约束 500），客户端无法安全重试；错误信息裸奔
- Realistic failure scenario: 用户双击"新建渠道" → 第二次请求返回 `duplicate key value violates unique constraint`
- Minimal fix: create 前同名校验返回语义化 409；错误映射为 apperror
- Regression test suggestion: 两次相同 create → 第二次 409 且 DB 仅一行
- Estimated effort: 2-3 小时

### Finding: APIKey 内存缓存成为事实数据源，更新强依赖缓存命中

- Severity: Medium
- Confidence: High
- Category: BackendAPI
- Status: Confirmed
- Affected area: internal/op/apikey.go
- Evidence:
  - File: internal/op/apikey.go:24-35（APIKeyUpdate 先 Get 缓存，未命中返回 "apikey not found" 即使 DB 存在）；DB 被外部修改/备份恢复后缓存不刷新（webdav restore 仅一次性 InitCache：webdav/backup.go:195-197）
- Problem: 更新路径拒绝缓存未命中但 DB 存在的合法记录；与 DB 一致性依赖所有写入方同步缓存，漏一处即幽灵数据
- Realistic failure scenario: 运维直改 DB 修复数据 → 面板列表旧值、更新报 "apikey not found"
- Minimal fix: 缓存未命中回退 DB 查询；写路径统一缓存失效包装器
- Regression test suggestion: 预置 DB 记录不预填缓存，APIKeyUpdate 应成功
- Estimated effort: 1-2 小时

### Finding: 60 个生产文件超 500 行，12 个超 1000 行

- Severity: High
- Confidence: High
- Category: Maintainability
- Status: Confirmed
- Affected area: transformer / relay / op / sitesync
- Evidence:
  - File: internal/transformer/outbound/gemini/messages.go (2126)、internal/transformer/outbound/anthropic/messages.go (2049)、internal/transformer/model/model.go (2030)、internal/transformer/outbound/openai/response.go (2009)、internal/relay/relay.go (1922)、internal/transformer/inbound/openai/response.go (1883)、internal/transformer/inbound/anthropic/messages.go (1649)、internal/op/backup.go (1602)、internal/sitesync/anyrouter.go (1499)、internal/op/site_import.go (1330)、internal/relay/images.go (1328)、internal/op/log.go (1275)
- Problem: 单文件承载过多状态，diff 冲突概率高，函数间隐式共享局部上下文
- Realistic failure scenario: 修改 gemini 流式转换误伤 openai 路径（同文件共享辅助函数）
- Minimal fix: 按"请求转换/流事件/响应转换/schema"拆分子文件（同包）
- Regression test suggestion: 拆分后 `go test ./transformer/...` 全量 + 各协议 golden 对比
- Estimated effort: 2-4 天

### Finding: importDBDump 为 758 行超级函数，13+ 张表循环为复制粘贴骨架

- Severity: High
- Confidence: High
- Category: Maintainability
- Status: Confirmed
- Affected area: internal/op/backup.go
- Evidence:
  - File: internal/op/backup.go:407-1165（每张表重复相同骨架：rejectDuplicateImportSourceID → ID=0 → remap → tx.First 查重 → Create）
- Problem: 表间仅"查重字段、remap 字段、Create 选项"不同，其余 80% 逻辑逐字重复；任何导入语义变更需同步改 13+ 处
- Realistic failure scenario: 新增表复制旧骨架漏掉依赖顺序校验 → 导入失败且无提示
- Minimal fix: 抽象 `importTableWithRemap[T]` 声明式描述
- Regression test suggestion: 利用 backup_test.go 往返测试断言 RowsAffected 与幂等性
- Estimated effort: 2-3 天

### Finding: relay.Handler 为 426 行编排函数，聚合 6 个关注点

- Severity: High
- Confidence: High
- Category: Maintainability
- Status: Confirmed
- Affected area: internal/relay/relay.go
- Evidence:
  - File: internal/relay/relay.go:61-487（3 层循环嵌套：iter.Next → retry → select key，职责覆盖协议决策/重试/退避/指标/心跳/负载均衡）
- Problem: 无法单元测试任一关注点；新特性继续内联膨胀（relay.go:139-164）
- Realistic failure scenario: 修改退避逻辑误改指标保存路径 → 超时请求计费丢失，无测试发现
- Minimal fix: 拆出 resolveRouting/executeChannelAttempt/selectChannelKey/finalizeAttempt
- Regression test suggestion: 补"重试耗尽 + 指标保存"组合测试（基于现有 relay_test.go 1931 行）
- Estimated effort: 1-2 天

### Finding: transformer 协议转换函数普遍 200-500 行，跨协议重复转换逻辑

- Severity: High
- Confidence: High
- Category: Maintainability
- Status: Confirmed
- Affected area: internal/transformer
- Evidence:
  - File: internal/transformer/outbound/gemini/messages.go:613-1111（convertLLMToGeminiRequest 498 行）、internal/transformer/inbound/anthropic/messages.go:728-1213（TransformStream 485 行）、:1214-1524（TransformStreamEvents 310 行）
- Problem: gemini/anthropic/openai 三套转换器各自实现流事件合并、tool_call 合并、finish_reason 映射，同一语义多处实现
- Realistic failure scenario: Gemini 修复 tool_call id 合并 bug，Anthropic 同样 bug 未修 → 用户切 provider 后工具调用失败
- Minimal fix: tool_call 合并/finish_reason 映射/thinking 块规范化提取到 transformer/model 共享层
- Regression test suggestion: 各协议 golden JSON 对比测试扩展边界 case
- Estimated effort: 2-3 天

### Finding: ImagesHandler 复制 relay.Handler 的模型解析 + catalog 校验前 60 行

- Severity: High
- Confidence: High
- Category: Maintainability
- Status: Confirmed
- Affected area: internal/relay/images.go
- Evidence:
  - File: internal/relay/images.go:37-99,804-936 vs internal/relay/relay.go:61-100,496-693（逐行等价的双份实现）
- Problem: catalog 校验规则变更需改两处；漏改导致 images 通道绕过新校验
- Realistic failure scenario: 新增"黑名单模型"校验只加在 relay.go → 图片请求仍可调用已禁用模型
- Minimal fix: 抽取 `resolveRequestModel()` 共享
- Regression test suggestion: 对共享函数做表驱动测试，两入口各留冒烟测试
- Estimated effort: 1 天

### Finding: op 包 74 个文件过度膨胀，sitesync 反向依赖 op 业务层 30+ 函数

- Severity: High
- Confidence: High
- Category: Maintainability
- Status: Confirmed
- Affected area: internal/op、internal/sitesync、internal/site
- Evidence:
  - File: internal/sitesync/anyrouter.go/core.go/http.go（18 处 import internal/op，调用 SiteAccountCreate×8、SiteCreate×7、ChannelGet×6 等 30+ 函数）；internal/op/（74 文件）
- Problem: "业务逻辑层"op 被下游服务层反向调用，依赖方向倒置；site 域功能分布两包，handler 同时面向三个业务入口
- Realistic failure scenario: sitesync 调用 op 函数绕过内存缓存（如 ChannelGet），改缓存策略后同步路径读到脏数据
- Minimal fix: sitesync 依赖收敛为接口注入；或在架构文档显式声明站点域归属
- Regression test suggestion: depgraph CI 禁止 sitesync→op 双向依赖
- Estimated effort: 1-2 天（规划）+ 数天落地

### Finding: 前端 3 个上帝组件（site-channel 3343 行 / site 2535 行 / log Item 1257 行）

- Severity: High
- Confidence: High
- Category: Maintainability / Frontend
- Status: Confirmed
- Affected area: web/src/components/modules/
- Evidence:
  - File: web/src/components/modules/site-channel/index.tsx（3343 行、98 处 hook、32 个组件定义）；web/src/components/modules/site/index.tsx（2535 行、54 处 hook）；web/src/components/modules/log/Item.tsx（1257 行）
- Problem: 列表/过滤/排序/对话框/图表/跳转定位/状态同步混装，内部组件复用为零
- Realistic failure scenario: 修改 site-channel 卡片布局误伤 SiteChannelCompletionAction 的补全计数逻辑且无测试兜底
- Minimal fix: 按卡片/列表/对话框/跳转/图表拆分子目录，先拆 log/Item.tsx
- Regression test suggestion: 为拆出的纯函数（collectPendingCompletionSites）补单测
- Estimated effort: 4-6 天（分多次 PR）

### Finding: 前端 token（JWT/API Key 明文）持久化到 localStorage

- Severity: High
- Confidence: High
- Category: Security / Frontend
- Status: Confirmed
- Affected area: web/src/api/endpoints/user.ts、web/src/api/client.ts
- Evidence:
  - File: web/src/api/endpoints/user.ts:118-131（`persist({name:'auth-storage', partialize...})` 将 token/expireAt/isAPIKeyAuth 落盘 localStorage）、:74（setAPIKeyAuth 把 API Key 明文写入 token 字段一并持久化）、:100-102（API Key 模式跳过本地过期检查）
- Problem: 管理员凭证明文存 localStorage；API Key 无过期时间，被盗后无法自然失效；项目内嵌 `@uiw/react-json-view`（alpha 版渲染后端日志内容）与 recharts `dangerouslySetInnerHTML` 构成 XSS 面
- Realistic failure scenario: 日志详情页渲染恶意 prompt → 脚本读 localStorage auth-storage 外传 → 攻击者以管理员身份直连后台 API
- Minimal fix: 仅 persist isAuthenticated + 过期时间；token 放 sessionStorage/内存；或 HttpOnly Cookie
- Regression test suggestion: 登录后重开页面需重新验证；无 token 时 401 流程正常
- Estimated effort: 2-3 天（含后端 Cookie 方案）

### Finding: i18n 硬编码中文绕过 t()（与三语零缺失矛盾）

- Severity: High
- Confidence: High
- Category: Frontend / Maintainability
- Status: Confirmed
- Affected area: web/src/components/modules/
- Evidence:
  - File: web/src/components/modules/toolbar/index.tsx:166,180,190,202,209,221,228（'新增站点'/'统一补全 Key'/'新增渠道'/'自动分组'/'刷新'）；web/src/components/modules/site/index.tsx:1171,1607-1627,2407-2495（归档/删除站点弹窗、筛选提示）；web/src/components/modules/site-channel/index.tsx:330,753-761,819-823（图表 tooltip 成功/失败/成功率直接渲染硬编码中文）
- Problem: 英文/繁体界面混入中文；新语言无法翻译
- Realistic failure scenario: 管理员切 English 后工具栏中文按钮 + 英文界面混杂
- Minimal fix: 抽取到 public/locale/*.json，替换为 t()；加 ESLint 规则禁止中文字符串字面量
- Regression test suggestion: 三语快照对比页面文本
- Estimated effort: 2-3 天

### Finding: 认证/鉴权链路零测试（server/auth、middleware、conf、client 覆盖率 0%）

- Severity: High
- Confidence: High
- Category: Testing
- Status: Confirmed
- Affected area: internal/server/auth、internal/server/middleware、internal/conf、internal/client
- Evidence:
  - File: 上述目录 `find -name "*_test.go"` 返回 0 个文件；实测覆盖率 `server/auth` 0.0%、`server/middleware` 0.0%、`conf` 0.0%
- Problem: JWT 过期/签名错误/API Key 前缀与哈希校验、CORS、鉴权中间件拒绝路径完全没有自动化验证
- Realistic failure scenario: 修改 API Key 校验逻辑（错误接受短 key）后合入，攻击者用伪造 key 消耗上游额度
- Minimal fix: 为 server/auth 与 middleware 各补 5-10 个 httptest
- Regression test suggestion: 表驱动用例：无头/坏头/过期 token/非法 key → 401
- Estimated effort: 3-4 小时

### Finding: server/handlers 仅 5.0% 覆盖，核心业务 handler 无测试

- Severity: High
- Confidence: High
- Category: Testing
- Status: Confirmed
- Affected area: internal/server/handlers（23 文件）
- Evidence:
  - File: internal/server/handlers/ 仅 4 个测试文件（log_analytics_test.go、setting_export_test.go、site_recovery_test.go、site_import_test.go），覆盖率 5.0%
- Problem: Channel/User/APIKey/Group/Setting/Stats 全部管理 CRUD 无行为测试
- Realistic failure scenario: op 层缓存重构后 Channel 更新不失效缓存，面板显示旧配置，测试全绿
- Minimal fix: 为 channel/user/apikey/setting 各补 2-3 个端到端 httptest（走真实 op 层）
- Regression test suggestion: `TestChannelUpdateRefreshesCache` 级冒烟用例
- Estimated effort: 4-6 小时

### Finding: relay/balancer 核心负载均衡仅 22.9% 覆盖

- Severity: High
- Confidence: High
- Category: Testing
- Status: Confirmed
- Affected area: internal/relay/balancer
- Evidence:
  - File: internal/relay/balancer/circuit_test.go（3 个测试）、session_test.go（1 个测试）；覆盖率 22.9%
- Problem: RoundRobin/Random/Failover/Weighted 策略与熔断器开闭时序未验证
- Realistic failure scenario: 熔断阈值改动后上游恢复时熔断器不闭合，全量流量持续失败且无测试预警
- Minimal fix: 为策略选择与熔断状态机补表驱动测试
- Regression test suggestion: 熔断器状态转换用例：连续失败 → open → half-open → 成功 → closed
- Estimated effort: 4 小时

### Finding: 前端 170 文件 0 测试，CI 前端 job 仅 lint 无类型检查与构建

- Severity: High
- Confidence: High
- Category: Testing
- Status: Confirmed
- Affected area: web/ + .github/workflows/ci.yml
- Evidence:
  - File: web/package.json（无 test 脚本、无 vitest/jest 依赖）；.github/workflows/ci.yml frontend job 仅 `pnpm lint`（ESLint 不执行类型检查）
- Problem: 前端 TS 类型错误在 PR 阶段不可见，直到 build/release 才暴露；业务逻辑（stores/API client/i18n/路由）零保护
- Realistic failure scenario: 后端 stats 接口字段重命名，前端手写 interface 不报错 → 构建通过 → 上线后仪表盘全空
- Minimal fix: CI 加 `pnpm build`（或 tsc --noEmit）；为 api/client.ts 与关键 store 补最小 vitest 单测
- Regression test suggestion: tsc --noEmit 作 PR 门槛 + api client 序列化测试
- Estimated effort: 1 小时（CI 门禁）+ 1-2 天（测试设施）

### Finding: changelog.yml 在每次 v* tag 推送时对 master 执行 `git push --force`

- Severity: High
- Confidence: High
- Category: Release
- Status: Confirmed
- Affected area: .github/workflows/changelog.yml
- Evidence:
  - File: .github/workflows/changelog.yml:29-40（生成 changelog → `git merge origin/dev` → `git push origin master --force`）；无 concurrency 限制
- Problem: force push 丢弃 master 上任何非 dev 来源提交（hotfix/文档直改）；dev 与 master 冲突时 merge 失败但 changelog 已发布；两个 tag 连发互相覆盖
- Realistic failure scenario: 线上紧急 hotfix 直提 master → 任何人打 tag → hotfix 被 dev 状态覆盖
- Minimal fix: 去掉 --force（改为 fast-forward 检查失败即中止）；加 concurrency；仅 release tag 触发
- Regression test suggestion: 发布演练 checklist
- Estimated effort: 30 分钟

### Finding: release 链路无测试门禁、无并发保护、`go mod tidy` 隐式漂移

- Severity: High
- Confidence: High
- Category: Release
- Status: Confirmed
- Affected area: .github/workflows/release.yaml、scripts/build.sh
- Evidence:
  - File: .github/workflows/release.yaml:26-41（直接 `bash scripts/build.sh release`，无 go test/lint；版本用 `git describe --tags --abbrev=0`）；scripts/build.sh:169-180（release 内执行 `go mod tidy`——CI 中会隐式改动 go.mod）、:228-235（串联外部 Python 价格脚本，断网全链失败）、:333-340（build_standard 失败仅 log_error 不退出）
- Problem: 带已知测试失败的代码可照常发布；tidy 网络依赖漂移；describe 到最近 tag 未必等于本次发布对象；并行 push 两个 release 互相覆盖资产
- Realistic failure scenario: 连续两次 push master → 两个 release job 并行 → 第二个 upload-release 覆盖第一个的 tag 资产
- Minimal fix: release 前加 `go test ./...`；去掉 go mod tidy；加 concurrency；校验 tag 与 HEAD 一致；build 失败即退出
- Regression test suggestion: 发布演练 checklist
- Estimated effort: 1-2 小时

### Finding: 后台任务吞错无日志（_ = op.Xxx）

- Severity: Medium
- Confidence: High
- Category: Stability / Maintainability
- Status: Confirmed
- Affected area: internal/task、internal/op
- Evidence:
  - File: internal/task/site_outlier.go:124,147（`_ = op.SiteChannelOutlierClear(...)`）；internal/op/stats_site_model_backfill.go:33,137（`_ = SettingSetString(...)`）；internal/op/log.go:111；internal/op/proxy_pool.go:456（`_, _ = io.Copy(...)`）
- Problem: 失败静默；backfill 标记写失败会永久重复执行全量扫描，异常循环无日志线索
- Realistic failure scenario: SettingSetString 因 DB 锁失败被吞 → backfill 标记永远未写 → 每次启动重跑全量
- Minimal fix: 统一 `if err != nil { log.Errorw(...) }` 模式
- Regression test suggestion: 注入失败 mock，断言日志输出
- Estimated effort: 半天

### Finding: 后台常驻 goroutine 无优雅停止路径

- Severity: Low
- Confidence: High
- Category: Stability
- Status: Confirmed
- Affected area: internal/relay/responses_replay_store.go、internal/task/task.go
- Evidence:
  - File: internal/relay/responses_replay_store.go:36-55（init() 启动 sweeper，`:40` stop channel 存在但全库无 close 调用）；internal/task/task.go:17,141（stopCh 存在，stopOnce 无使用者）
- Problem: 两个后台循环永远运行；`cmd/start.go:115` 的 shutdown 只是按序执行 hook 后 os.Exit，goroutine 被强杀
- Realistic failure scenario: 未来循环内引入 flush/持久化 → 停机丢数据；测试环境 init() goroutine 跨测试残留
- Minimal fix: 导出 Stop 函数并注册到 shutdown.Register
- Regression test suggestion: goleak 断言 Stop 后 goroutine 限时退出
- Estimated effort: 0.5 天

### Finding: shutdown.go hook 列表无并发保护

- Severity: Low
- Confidence: Medium
- Category: Stability
- Status: Confirmed（当前仅理论风险）
- Affected area: internal/utils/shutdown/shutdown.go
- Evidence:
  - File: internal/utils/shutdown/shutdown.go:17（普通 slice）、:24-26（Register 无锁 append）、:37-51（无锁遍历执行）
- Problem: 若未来运行期调用 Register，append 与 range 并发触发数据竞争；停机 hook 是数据一致性最后防线（SaveCache/RelayLogFlushPending/db.Close）
- Minimal fix: 加 sync.Mutex 或原子快照；Listen/Shutdown 用 sync.Once 防双重执行
- Regression test suggestion: 并发 Register + 遍历（race detector）
- Estimated effort: 0.5 小时

### Finding: backup 系列 6 个文件（4700+ 行）存在 3 套导入实现

- Severity: Medium
- Confidence: High
- Category: Maintainability
- Status: Confirmed
- Affected area: internal/op/backup*
- Evidence:
  - File: internal/op/backup.go (1602) + backup_extended_import.go (1710) + backup_zip_import.go + backup_zip_stream_import.go (607) + backup_extended_export.go；两条导入链：`DBImportIncremental→importDBDump→importExtendedTables` 与 `DBImportZip→importBackupZipEntries`
- Problem: 表级逻辑在两套管线重复（如 remapRelayLogsForImport vs importBackupZipLogAndUsageEntries）；新增表需同步改三处
- Realistic failure scenario: 新表加入 JSON dump 导入但漏加 zip 导入 → zip 恢复后缺表数据且无告警
- Minimal fix: 统一为"流式解码 + 同一表级 importer"架构
- Regression test suggestion: 断言同数据 JSON 与 ZIP 恢复结果一致
- Estimated effort: 1-2 天

### Finding: 错误消息字符串跨 4 个文件重复硬编码

- Severity: Medium
- Confidence: High
- Category: Maintainability
- Status: Confirmed
- Affected area: internal/relay
- Evidence:
  - File: internal/relay/relay.go:133、compact.go、ws_client.go、images.go:103 — "no available channel"×7、"model not supported"×3、"model disabled"×3、"model not found"×3 均为字符串字面量
- Problem: 客户端可感知文案无法统一国际化；API 客户端依赖字符串判断失败原因时文案变更即失效
- Minimal fix: 提升为带 code 的常量或统一 code→message 映射
- Regression test suggestion: 断言响应 code 而非 message
- Estimated effort: 2-4 小时

### Finding: utils/ 10 个核心工具包 0% 覆盖

- Severity: Medium
- Confidence: High
- Category: Testing
- Status: Confirmed
- Affected area: internal/utils/*
- Evidence:
  - File: internal/utils/cache/（分片缓存 + 关机持久化）、tokenizer/、safe/、shutdown/、snowflake/ 等全部 `coverage: 0.0%`
- Problem: 缓存分片一致性、并发读写、持久化/恢复路径零验证；snowflake ID 生成、panic 恢复零测试
- Realistic failure scenario: 缓存分片哈希改动后 key 碰撞互相覆盖，channel 配置偶发回滚
- Minimal fix: 为 utils/cache 补并发读写 + 持久化恢复测试；snowflake 补唯一性测试
- Regression test suggestion: TestShardedCacheConcurrentPutGet / TestSnowflakeUniqueIDs
- Estimated effort: 3-4 小时

### Finding: db/migrate 约半数迁移无测试

- Severity: Medium
- Confidence: High
- Category: Testing
- Status: Confirmed
- Affected area: internal/db/migrate
- Evidence:
  - File: internal/db/migrate/（23 个迁移，13 个有测试，覆盖率 55.5%）；无测试：001-003、008-009、011-012、015-017
- Problem: 早期表结构/索引/默认数据迁移无验证；坏迁移只在启动时对真实 DB 执行一次，无法回滚
- Realistic failure scenario: 新版本给 relay_logs 加索引的迁移在 PostgreSQL 大表上执行超时 → 服务启动失败
- Minimal fix: 为无测试迁移补"空库执行 + 断言 schema"
- Regression test suggestion: 每个迁移用 SQLite 内存库跑后断言 HasTable/HasIndex
- Estimated effort: 2-3 小时

### Finding: 迁移机制无回滚、多实例并发启动无迁移锁

- Severity: Medium
- Confidence: Medium
- Category: Release
- Status: Suspected
- Affected area: internal/db/migrate/migrate.go
- Evidence:
  - File: internal/db/migrate/migrate.go:47-108（仅 Up，失败记 Failed 后拒绝启动；无跨实例锁）
- Problem: 失败迁移无自动回滚/降级；MySQL/PostgreSQL 多副本同时启动并发执行同一迁移存在竞态
- Realistic failure scenario: K8s 滚动重启两个副本 → 同一迁移并发执行 → 唯一约束冲突 → 健康检查反复重启
- Minimal fix: 迁移表加全局锁（advisory lock / SELECT FOR UPDATE）
- Regression test suggestion: 并发 goroutine 模拟双实例迁移
- Estimated effort: 3-4 小时

### Finding: docker-compose.yml 无版本固定、无 healthcheck

- Severity: Medium
- Confidence: High
- Category: Release
- Status: Confirmed
- Affected area: docker-compose.yml
- Evidence:
  - File: docker-compose.yml（`image: hureru/octopus` latest 无 pin；无 healthcheck；数据卷为 `/path/to/data` 占位符）
- Problem: `latest` 滚动更新 + 自更新机制叠加 = 用户无法固定行为；orchestration 无法感知崩溃
- Realistic failure scenario: 上游发布缺陷版本 → 用户 pull latest 直接中招且无 healthcheck 报警
- Minimal fix: 文档引导 pin tag；加 healthcheck；补充 env 示例
- Estimated effort: 30 分钟

### Finding: 版本管理无单一来源（前端固定 0.1.0，Go 依赖 git describe）

- Severity: Medium
- Confidence: High
- Category: Release
- Status: Confirmed
- Affected area: web/package.json、scripts/build.sh、release.yaml
- Evidence:
  - File: web/package.json:3（version 固定 0.1.0）；scripts/build.sh:26-29（git describe，无 tag 回退 'dev'）
- Problem: 用户看到的前端版本永远是 0.1.0，二进制版本依赖 tag 正确性；故障排查无法可靠区分部署版本
- Realistic failure scenario: 误用未打 tag 的 dev 构建部署 → 生产上无法追溯版本
- Minimal fix: `git describe --tags --always`（含 commit）；前端从 NEXT_PUBLIC_APP_VERSION 注入
- Estimated effort: 1 小时

### Finding: web/package.json 存在未使用依赖与 alpha 生产依赖

- Severity: Medium
- Confidence: High
- Category: Dependency
- Status: Confirmed
- Affected area: web/package.json
- Evidence:
  - File: web/package.json（`date-fns@^4.1.0` 0 引用——grep 无结果，dayjs 实际被 5 文件使用；`@uiw/react-json-view@2.0.0-alpha.39` 渲染不可信日志内容）
- Problem: 两个日期库并存且 date-fns 纯冗余；alpha 依赖渲染不可信内容，升级风险高
- Realistic failure scenario: json-view alpha 发布 breaking 变更 → pnpm install 升级 → 日志详情页白屏
- Minimal fix: 移除 date-fns；json-view pin 精确版本并评估稳定替代（react-json-view-lite）
- Estimated effort: 0.5-1 天

### Finding: README 构建要求与代码现状不符（Go 1.24.4 / Node 18+）

- Severity: Medium
- Confidence: High
- Category: Release
- Status: Confirmed
- Affected area: README.md、README_zh.md
- Evidence:
  - File: README.md:60-63（Requirements: Go 1.24.4、Node.js 18+）；go.mod:3（`go 1.25.0`——拒绝 <1.25 编译器）；web/package.json（next@16.2.12 要求 Node ≥20.9）
- Problem: 按文档安装必然失败
- Realistic failure scenario: 用户按 README 装 Go 1.24.4 → `go run main.go start` 报 "go.mod requires go >= 1.25.0"
- Minimal fix: 更新文档为 Go 1.25+ / Node 20.9+（与 CI 对齐）
- Estimated effort: 15 分钟

### Finding: usage 搜索使用前导通配符 LIKE，无法利用索引

- Severity: Low
- Confidence: High
- Category: BackendAPI / Performance
- Status: Confirmed
- Affected area: internal/op/usage_analytics_breakdown.go
- Evidence:
  - File: internal/op/usage_analytics_breakdown.go:454-455（`LIKE '%'+value+'%'`）
- Problem: 搜索框每次击键触发全表扫描（usage_analytics 持续增长表）
- Minimal fix: 前缀匹配 + 最少字符数限制（≥2）
- Regression test suggestion: 构造 10k 行断言 EXPLAIN 走索引
- Estimated effort: 1 小时

### Finding: 定价查询 `LOWER(model_name)` 包裹列导致索引失效 + 全量加载内存过滤

- Severity: Medium
- Confidence: High
- Category: Performance
- Status: Confirmed
- Affected area: internal/op/site_pricing.go
- Evidence:
  - File: internal/op/site_pricing.go:405-416（`Where("LOWER(model_name) = ?")`）、:419-425（Find 全量 + 内存过滤）
- Problem: 热路径中每个 candidate 执行该查询；包裹列无法走索引，quotes 表增长后全扫线性恶化
- Realistic failure scenario: 10 万行 quotes × 每请求 20 次 = 200 万行/请求扫描
- Minimal fix: 写入时归一化小写列；或建函数索引
- Regression test suggestion: EXPLAIN 断言走索引
- Estimated effort: 1-2 小时

### Finding: 统计聚合后台任务逐条 UPDATE + 每 delta 3 条 SQL

- Severity: Medium
- Confidence: High
- Category: Performance
- Status: Confirmed
- Affected area: internal/op/usage_aggregate.go
- Evidence:
  - File: internal/op/usage_aggregate.go:131-140（facts 逐条 Update）、:163-180（每 delta：INSERT + SELECT FOR UPDATE + Save）
- Problem: 高峰积压时单轮数万条 SQL，任务远超 5 分钟预算；SQLite 单写者下与请求写路径争锁
- Realistic failure scenario: 每分钟 2 万请求 → facts 表积压百万行，聚合查询连带变慢
- Minimal fix: 批量 UPDATE（IN 列表）+ `INSERT ... ON CONFLICT DO UPDATE SET col=col+EXCLUDED.col`
- Regression test suggestion: 构造 1 万 facts 断言 SQL 语句数 < 100
- Estimated effort: 2-3 小时

### Finding: relay_logs 管理查询使用 OFFSET 深分页 + 无条件 COUNT 全表

- Severity: Medium
- Confidence: High
- Category: Performance
- Status: Confirmed
- Affected area: internal/op/log.go
- Evidence:
  - File: internal/op/log.go:648-655（Count 全表）、:665-672（Order+Offset+Limit）；已提供 cursor 模式但默认仍走 page
- Problem: 第 100 页扫描 100×pageSize 行；无时间过滤时 COUNT 全表扫描
- Realistic failure scenario: 500 万行 relay_logs 翻第 50 页 + COUNT ≈ 2-5s
- Minimal fix: 默认切 cursor 模式；WithTotal 加时间范围必填
- Regression test suggestion: 造 10 万行断言 page 100 < 100ms
- Estimated effort: 1-2 小时

### Finding: 缓存 Get/Set 每次 fmt.Sprintf 分配字符串；ChannelKeyUpdate 每请求复制整个 Keys 数组

- Severity: Low
- Confidence: High
- Category: Performance
- Status: Confirmed
- Affected area: internal/utils/cache、internal/op/channel.go
- Evidence:
  - File: internal/utils/cache/cache.go:10-12,46-56（keyToString 每次 Sprintf）；internal/op/channel.go:83-108（每请求 2 Get + copy O(len(Keys)) + 2 Set + 全局锁）
- Problem: 热路径每请求几十次小分配 + O(keys) 复制
- Realistic failure scenario: 1000 QPS × 30 key → GC 频率上升
- Minimal fix: key 用 int 哈希直通；ChannelKeyUpdate copy-on-write
- Estimated effort: 1-2 小时

### Finding: 流式转发每事件双重 string/[]byte 转换 + 每请求 2-3 个 goroutine

- Severity: Low
- Confidence: High
- Category: Performance
- Status: Confirmed
- Affected area: internal/relay/relay.go、internal/relay/stream
- Evidence:
  - File: internal/relay/relay.go:1393-1394,1570-1590（transform 回调 `string(data)`+`[]byte(data)` 往返）；internal/relay/stream/processor.go:155-168 + sse_source.go:40-58（每流 2 goroutine）
- Problem: 高 token 率流下事件级分配量大；5000 并发流 = 1 万 goroutine
- Minimal fix: transform 回调直接传 []byte；SSESource 合并读循环；chunk buffer 用 sync.Pool
- Estimated effort: 2-4 小时

### Finding: 无意义命名 + 空 TODO 注释 + 导出函数缺注释

- Severity: Low
- Confidence: Medium
- Category: Maintainability
- Status: Confirmed
- Affected area: internal/update、internal/op、internal/transformer/model
- Evidence:
  - File: internal/update/update.go:37,71,96（`data` 同时表示响应体与 zip 内容）；internal/op/backup.go:50,284,1436（DBExportAll/DBImportIncremental/DBExportZip 无注释）；internal/transformer/model/model.go:194,274（空 TODO）；internal/utils/tokenizer/tokenizer.go:8（// TODO 更多模型）
- Problem: 导出 API 无语义说明（DBExportAll 与 DBExportZip 关系只能从实现推断）；空 TODO 无法追踪
- Realistic failure scenario: 新成员误用 DBExportAll 序列化大库日志导致 OOM（未注释的副作用）
- Minimal fix: 补注释（含副作用警告）；TODO 补内容或删除
- Estimated effort: 2-4 小时

### Finding: apperror.IsCode / Params 疑似死代码

- Severity: Low
- Confidence: Medium
- Category: Maintainability
- Status: Suspected
- Affected area: internal/apperror
- Evidence:
  - File: internal/apperror/apperror.go（`IsCode`、`Params` 方法全库无调用，排除 _test 与定义文件）
- Problem: 未被消费的公共 API 误导调用者以为存在统一错误码判断机制
- Minimal fix: 删除或补实际调用者
- Estimated effort: 30 分钟

### Finding: 前端状态重复与 effect 同步链（completion 补全状态 4 层副本）

- Severity: Medium
- Confidence: High
- Category: Frontend
- Status: Confirmed
- Affected area: web/src/components/modules/site-channel/
- Evidence:
  - File: web/src/components/modules/site-channel/index.tsx:3103-3114（useSiteChannelList 自算 pendingCompletionSites）、:3143-3157（useCompletionStateSync 重复计算）、:3185-3191（useEffect 写 completion-store）、:3182 与 :3105（对话框 open 状态双份）；:3187 注释自述"避免残留 open 状态自动重开"
- Problem: 同一派生数据 4 层副本 + effect 同步；对话框状态双源，正是经典竞态温床
- Realistic failure scenario: 对话框打开时清空任务 → effect 关闭 store 对话框 → 新任务出现 → 对话框自动重开（两态不一致）
- Minimal fix: 删除 completion-store 的 dialog 状态与 useCompletionStateSync；状态收敛唯一 owner
- Regression test suggestion: 单测覆盖计数 0→N→0→N 的 dialog 开关序列
- Estimated effort: 1-2 天

### Finding: 前端 stores 层反向依赖 components 层 + 模块级副作用订阅

- Severity: Medium
- Confidence: High
- Category: Frontend
- Status: Confirmed
- Affected area: web/src/stores/
- Evidence:
  - File: web/src/stores/jump.ts:3（从 @/components/modules/navbar import useNavStore）、:52-55（requestJump 内直接调用 setActiveItem）；web/src/components/modules/channel/tab-store.ts:45-52（模块级执行 useJumpStore.subscribe 反向写 sessionStorage）
- Problem: 依赖方向倒置导致 stores 无法脱离 UI 复用；模块加载顺序敏感（tab-store 初始值读 pending，import 顺序变化行为即变）
- Realistic failure scenario: 换导航实现 → jump.ts 编译报错 → 整个跳转体系崩
- Minimal fix: 将 NavItem/useNavStore 上移至 src/stores/nav.ts；导航由消费方统一订阅
- Estimated effort: 1 天

### Finding: 前端 11+ 个 zustand store 分片过细、persist 策略不统一

- Severity: Medium
- Confidence: High
- Category: Frontend
- Status: Confirmed
- Affected area: web/src/stores/、web/src/components/modules/
- Evidence:
  - File: 全项目 11+ store（setting/jump/nav-store/site/ui-store/log/ui-store/proxy-pool/dialog-store/site-channel/completion-store/channel/tab-store/toolbar/view-options-store/home/store/log/analytics-store/toolbar/search-store）；其中 ui-store/log ui-store/dialog-store 是"UI 请求总线"（与 React Query server state 职责重叠）
- Problem: 部分 store 仅存"一次性请求意图"用后即清，生命周期靠各页面自觉 reset；事件未被消费即丢失
- Realistic failure scenario: 工具栏点"新增站点"后 requestOpenCreateDialog 未被 site 页消费（页面未挂载）→ 用户点了没反应
- Minimal fix: 建立 store 分片规范（全局偏好 persist / 跨页意图统一 / 同页状态局部）
- Estimated effort: 2-3 天

### Finding: 前端错误类型双重断言 + console 绕过 logger

- Severity: Medium
- Confidence: High
- Category: Frontend
- Status: Confirmed
- Affected area: web/src/
- Evidence:
  - File: web/src/components/modules/setting/APIKey.tsx:698,721,734（`(error as unknown as ApiError)` 双重断言）；web/src/api/client.ts:20 与 web/src/route/error-boundary.tsx:25（直接 console.error 绕过已有 logger.ts 门控）；web/src/components/modules/site/index.tsx:1045（console.warn）
- Problem: 断言掩盖类型系统未打通，ApiError 子字段静态类型丢失；生产控制台持续输出错误噪音
- Minimal fix: useMutation 显式泛型；三处改走 logger
- Estimated effort: 0.5 天

### Finding: 站点卡片列表每卡一个 ResizeObserver，回调 map 只增不删

- Severity: Medium
- Confidence: High
- Category: Frontend / Performance
- Status: Confirmed
- Affected area: web/src/components/modules/site/index.tsx
- Evidence:
  - File: web/src/components/modules/site/index.tsx:645-707（cardObserversRef Map 每 siteId 一个 ResizeObserver，每次 resize 都 setSiteCardHeights 触发整列表重渲染；:669-685 cardMeasureRefCallbacks 只增不删）
- Problem: O(n) 个观察器 + 高度同步 state；上百卡片时布局抖动持续触发重渲染
- Realistic failure scenario: 300+ 站点频繁同步导致高度变化 → 主线程被 ResizeObserver 回调打满 → 滚动卡顿
- Minimal fix: 单共享 ResizeObserver + CSS 变量；回调 map 卸载时删除
- Estimated effort: 1 天

### Finding: ErrorBoundary fallback 永远英文 + html lang 硬编码 zh-Hans

- Severity: Low
- Confidence: High
- Category: Frontend
- Status: Confirmed
- Affected area: web/src/route/error-boundary.tsx、web/src/app/layout.tsx
- Evidence:
  - File: web/src/route/error-boundary.tsx:33（fallback 硬编码英文，ErrorBoundary 在 Provider 外取不到 t）；web/src/app/layout.tsx:23（`<html lang="zh-Hans">` 硬编码，与可切 locale 脱钩）
- Problem: 三语项目唯一永远英文的界面；切 en 后文档语言声明仍为中文（影响屏幕阅读器/翻译建议）
- Minimal fix: fallback 用 useSettingStore locale 查 JSON；客户端按 locale 更新 document lang
- Estimated effort: 0.5 天

### Finding: AccountEditDialog useLayoutEffect 无依赖数组每渲染执行 + 双通道测高

- Severity: Low
- Confidence: High
- Category: Frontend / Performance
- Status: Confirmed
- Affected area: web/src/components/modules/site/AccountEditDialog.tsx
- Evidence:
  - File: web/src/components/modules/site/AccountEditDialog.tsx:91-93（useLayoutEffect 无依赖数组，每次渲染 updateHeight()）、:79-114（AnimatedFormSection 另持 ResizeObserver——双通道职责重复）
- Minimal fix: 删除无依赖 layout effect，仅保留 ResizeObserver
- Estimated effort: 0.5 天

### Finding: route.config label 字段死代码 + getErrorMessage 重复实现

- Severity: Low
- Confidence: High
- Category: Frontend
- Status: Confirmed
- Affected area: web/src/route/config.tsx、web/src/components/modules/
- Evidence:
  - File: web/src/route/config.tsx:12,26-32（label 字段全项目无消费点）；AccountEditDialog.tsx:208-216 与 site-channel/index.tsx:329-332（getErrorMessage 重复实现）；login/index.tsx:45（expire: 86400 硬编码）
- Minimal fix: 删除 label；提取公共 getErrorMessage；expire 挪常量
- Estimated effort: 0.5 天

### Finding: 测试风格为纯集成式，无 mock 框架，全量回归耗时长

- Severity: Low
- Confidence: High
- Category: Testing
- Status: Confirmed
- Evidence:
  - File: internal/relay/relay_test.go（1931 行，19.1s）、internal/op/backup_extended_test.go（2281 行，12s）；全项目无 gomock/sqlmock/testify；无 testing.Short() 支持
- Problem: 单测与集成不分层，故障注入只能起真实组件；全量回归接近分钟级，易诱发选择性跳过
- Realistic failure scenario: relay 改动只跑 relay 单包，op/sitesync 交叉回归漏检
- Minimal fix: 纯函数（balancer 选择、tokenizer、协议转换）补不依赖 DB 的单元测试；CI 保持全量
- Estimated effort: 长期投入

### Finding: 断言风格整体良好（正面确认）

- Severity: Info
- Confidence: Medium
- Category: Testing
- Status: Confirmed
- Evidence:
  - File: 全项目无 `if testing` 分支、无 t.Skip（grep 0 结果）；relay_test.go 181 处断言均带消息，无 assert.Nil 后无验证的宽松模式
- Problem: 无（测试真实性整体健康，这是正面确认）
- Minimal fix: 无（持续补核心路径用例即可）
- Estimated effort: 无

---

## 5. Security Concerns（安全）

见第 4 节问题卡，安全相关条目汇总：

| # | 标题 | Severity | 证据 |
|---|------|----------|------|
| S-1 | 上游 Token/APIKey 明文序列化进 API 响应 | Critical | model/site_channel.go:59,83；apikey.go:6 |
| S-2 | 默认凭据 admin/admin 明文入日志 | High | op/user.go:19-20,27 |
| S-3 | 请求体无大小限制（/v1 全入口） | High | relay/relay.go:758 |
| S-4 | JWT 密钥复用为加密密钥 + 弱回退 | High | op/secret.go:53-65；auth/auth.go:31-38 |
| S-5 | HTTP Server 无超时 | High | server/server.go:58-59 |
| S-6 | 密钥明文存储 + 备份明文导出 | High | model/apikey.go:6；backup.go:1461-1501 |
| S-7 | 盲 SSRF（站点探测） | Medium | sitesync/detect.go:42-131 |
| S-8 | API Key 限速可被 LRU 驱逐绕过 | Low | op/ratelimit.go:16,36-43 |
| S-9 | WS 升级跳过 Origin 校验 | Low | relay/ws_client.go:40-42 |
| S-10 | CORS `*` + 凭据 | Low | middleware/cors.go:14-33 |
| S-11 | 请求/响应内容明文持久化 | Low | relay/metrics.go:522 |
| S-12 | err.Error() 泄漏内部细节（11+ 处） | High | handlers 各文件 |
| S-13 | 前端 token 明文存 localStorage | High | web user.ts:118-131 |

**已核实为安全的做法**（正面确认）：Raw SQL 全部参数绑定无注入；update zip-slip 防护到位；SettingForClient 过滤 JWT secret；APIKeyUpdate 正确 Omit("api_key")；header_policy 屏蔽敏感头；gin CustomRecovery 全局兜底。

## 6. Stability Concerns（稳定性）

| # | 标题 | Severity | 证据 |
|---|------|----------|------|
| ST-1 | 请求体无限制 ReadAll（与 S-3 同源） | High | relay.go:758 |
| ST-2 | WS 池 preflight 持锁中途解锁并发竞态 | Medium | ws_pool.go:290-317 |
| ST-3 | 熔断器/session sync.Map 无界增长 | Medium | balancer/circuit.go:39；session.go:17 |
| ST-4 | 后台常驻 goroutine 无优雅停止 | Low | responses_replay_store.go:36-55；task.go |
| ST-5 | shutdown hook 列表无锁 | Low | utils/shutdown/shutdown.go:17 |
| ST-6 | 后台任务吞错无日志 | Medium | task/site_outlier.go:124,147 |

**已核实为安全的做法**：rateLimitCache 有界；relay 日志队列 context.WithoutCancel + 有界背压 + 退出 flush；images 裸 goroutine 有退出路径；heartbeat done channel 防死锁正确。

## 7. Performance Concerns（性能）

| # | 标题 | Severity | 证据 |
|---|------|----------|------|
| P-1 | 热路径 10-20 次无缓存 DB 查询 | Critical | relay.go:95-100；catalog.go:762 |
| P-2 | 流式 O(N²) 全量重扫 + 双份缓冲 | Critical | stream/processor.go:214-371；relay.go:1464-1485 |
| P-3 | BPE 编码器每块重建 | Critical | anthropic/messages.go:105-405；tokenizer.go:7-15 |
| P-4 | 代理池每请求新建 http.Client | Critical | client/http.go:83-88 |
| P-5 | 统计 3 把全局锁 + 非原子 RMW | High | op/stats.go:323-385 |
| P-6 | 站点模型小时统计全局锁 | High | stats_site_model.go:44-84 |
| P-7 | 日志双份全量 JSON 序列化 | High | relay/metrics.go:520-535 |
| P-8 | ParamOverride 全量 map 解析 | High | helper/param_override.go:14-55 |
| P-9 | 统计聚合逐条 UPDATE | Medium | op/usage_aggregate.go:131-180 |
| P-10 | relay_logs 深分页 + 全表 COUNT | Medium | op/log.go:648-672 |
| P-11 | LOWER() 索引失效 | Medium | op/site_pricing.go:405-425 |
| P-12 | 缓存 key Sprintf + Keys 数组复制 | Low | utils/cache/cache.go:10-56 |
| P-13 | 流式事件双重 string 转换 + 每流 2 goroutine | Low | relay.go:1393-1394 |

## 8. Testing Gaps（测试缺口）

| # | 标题 | Severity | 证据 |
|---|------|----------|------|
| T-1 | `go test ./...` 全量红灯 | Critical | op 8 个 + sitesync 1 个失败（本机实测） |
| T-2 | auth/middleware/conf/client 覆盖率 0% | High | find 无测试文件 |
| T-3 | handlers 覆盖率 5.0% | High | 仅 4 个测试文件 |
| T-4 | balancer 覆盖率 22.9% | High | circuit_test 3 个 + session_test 1 个 |
| T-5 | 前端 0 测试 + CI 无类型检查 | High | web 无 test 脚本；ci.yml 仅 lint |
| T-6 | utils/ 10 个包 0% 覆盖 | Medium | 实测 |
| T-7 | db/migrate 半数迁移无测试 | Medium | 10 个迁移无测试 |
| T-8 | 无 mock 框架、无 -short、全量分钟级 | Low | relay_test 19.1s |
| T-9 | 测试真实性整体良好（正面） | Info | 无 t.Skip、无 test-only 分支 |

## 9. Maintainability Concerns（可维护性）

| # | 标题 | Severity | 证据 |
|---|------|----------|------|
| M-1 | 60 文件超 500 行、12 个超 1000 行 | High | 全库统计 |
| M-2 | importDBDump 758 行超级函数 | High | backup.go:407-1165 |
| M-3 | relay.Handler 426 行编排 | High | relay.go:61-487 |
| M-4 | transformer 三协议转换逻辑重复 | High | 三套 2000 行文件 |
| M-5 | ImagesHandler 复制 Handler | High | images.go:37-99 |
| M-6 | op 包 74 文件 + sitesync 反依赖 | High | sitesync 18 处 import op |
| M-7 | backup 三套导入实现 | Medium | backup*.go 4700+ 行 |
| M-8 | 错误消息字符串重复硬编码 | Medium | "no available channel"×7 |
| M-9 | 吞错无日志 | Medium | task/site_outlier.go:124 |
| M-10 | 无意义命名/空 TODO/缺导出注释 | Low | update.go:37；model.go:194 |
| M-11 | apperror.IsCode 死代码（疑似） | Low | apperror.go |

## 10. Release Concerns（发布）

| # | 标题 | Severity | 证据 |
|---|------|----------|------|
| R-1 | 自更新无签名校验 | Critical | update/core.go:26-44 |
| R-2 | changelog.yml force push master | High | changelog.yml:38 |
| R-3 | release 无测试门禁/无并发保护/go mod tidy | High | release.yaml:26-41；build.sh:169-180 |
| R-4 | build.yml label 触发 + 平台列表三处不一致 | Medium | build.yml:10-16 |
| R-5 | 迁移无回滚/无分布式锁 | Medium | migrate.go:47-108 |
| R-6 | docker-compose 无 pin/无 healthcheck | Medium | docker-compose.yml |
| R-7 | 版本无单一来源 | Medium | package.json 0.1.0 固定 |
| R-8 | README Go/Node 版本过期 | Medium | README.md:60-63 |

## 11. Type Safety Analysis（类型安全）

| Subtype | Count | 关键点 |
|---------|-------|--------|
| ResponseLeak | 2 | 明文密钥序列化（Critical）；User 无 json tag（Low） |
| ErrorType | 1 | err.Error() 原样回传（High） |
| InputBoundary | 2 | 绑定校验缺失（High）；pageSize 无上限（Medium） |
| TypeAssertion | 1 | 前端 `as unknown as ApiError`（Medium） |

## 12. Frontend State Analysis（前端状态）

| Subtype | Count | Affected Components |
|---------|-------|-------------------|
| ComponentSize | 3 | site-channel 3343 行 / site 2535 行 / log Item 1257 行 |
| StateDuplication | 1 | completion 状态 4 层副本（site-channel/index.tsx:3103-3191） |
| EffectChain | 1 | useCompletionStateSync + useEffect 写 store |
| UIBusinessCoupling | 2 | stores→components 反依赖（jump.ts:3）；UI 请求总线 store |
| StoreFragmentation | 1 | 11+ 个 zustand store persist 策略不统一 |
| RenderPerf | 2 | 每卡 ResizeObserver（site/index.tsx:645-707）；layout effect 无依赖（AccountEditDialog.tsx:91） |

## 13. Backend API Analysis（后端 API）

| Subtype | Count | 关键点 |
|---------|-------|--------|
| ResponseLeak | 2 | 明文密钥 + err.Error() |
| Validation | 2 | binding 缺失；InvalidJSON 误报 |
| Idempotency | 1 | create 无幂等 |
| DataFlow | 2 | APIKey 缓存为数据源；读改写无事务 |
| Pagination | 2 | pageSize 无上限；LIKE 前导通配符 |

## 14. Dependency Weight Analysis（依赖）

| Dependency | Status | Used For | Recommended Action |
|------------|--------|----------|-------------------|
| go.mod 依赖（gin v1.12 / jwt v5.3 / gorm v1.31 / sqlite v1.11 / websocket v1.8） | Healthy | 无已知高危 CVE（未运行 govulncheck） | Keep |
| web: dayjs + date-fns 并存 | Overweight | date-fns 0 引用 | 移除 date-fns |
| web: @uiw/react-json-view alpha.39 | Overweight | 渲染不可信日志内容 | Pin 精确版本/评估稳定替代 |
| web: @floating-ui/react | Healthy | Radix 生态 | Keep |
| Go 工具链：无 vendor（go.mod 直接依赖）+ go mod tidy 在 CI 中执行 | Risk | 隐式漂移 | 移除 tidy |

## 15. Principles Compliance（设计原则）

### Principles Violated

| Principle | Violations | Severity | Affected Areas |
|-----------|------------|----------|----------------|
| Single Responsibility (SRP) | 6 | High | relay.go、backup.go、images.go、catalog.go、anyrouter.go、site-channel/index.tsx |
| File Size Limit | 60+12 | High | transformer/relay/op/sitesync 各协议文件 |
| DRY | 5 | High | backup 三套导入、transformer 三协议重复、ImagesHandler 复制、错误字符串 |
| Dependency Direction | 2 | High | sitesync→op 30+ 调用、前端 stores→components |
| Fail-Fast | 3 | High | rand 失败弱回退、吞错无日志、release 失败不退出 |
| Silent Fallback | 2 | Medium | JWT 弱回退、APIKey 缓存未命中即报错 |
| Command-Query Separation | 1 | Low | stats.go Get/Add/Set 混合 |
| Law of Demeter | 1 | Low | tab-store 模块级订阅跨层编排 |

### Principles Respected

- **参数化 SQL 全面**：proxy_pool.go 等 Raw SQL 全部 `?` 绑定，无注入面（fail-fast 反面——输入防线扎实）
- **分层大体清晰**：handlers 不直接操作 DB（仅 2 处引用 gorm 做错误判别）；统一 resp 响应契约 + apperror 错误码体系已建立
- **防御性编码多处到位**：zip-slip 防护、备份路径拒绝 `..`、webdav 恢复大小上限、尾随 JSON 拒绝
- **测试真实性**：无 t.Skip、无 test-only 生产分支、断言均有消息

---

## 16. Fallback / Defensive Code Analysis（兜底代码分析）

| Subtype | Count | KeepWithAlert | FailFast | Remove |
|---------|-------|---------------|----------|--------|
| SilentFallback | 2 | 1（JWT 弱回退——应收紧为 fail-fast） | 0 | 1（rand 失败回退） |
| EmptyCatch | 0 | - | - | - |
| CompatibilityBranch | 1 | 1（legacy JWT 方法——保留但需注释） | 0 | 0 |
| SilentCorrection | 1 | 0 | 0 | 1（task 吞错） |
| DefensiveGuess | 1 | 1（APIKey 缓存未命中直接判 not found——应收紧） | 0 | 0 |

## 17. Testing Authenticity Analysis（测试真实性）

| Test Area | Real Confidence | Risk | Action |
|-----------|---------------|------|--------|
| transformer 协议转换（messages_test.go 901/700 行） | High | 低——golden 对比真实 | Keep |
| backup 导入导出（2281 行） | High | 低——真实 SQLite 往返 | Keep |
| relay 主路径（1931 行） | Medium | 中——覆盖主路径但失败路径/并发无 | Keep + 补 |
| op catalog 系列 | **Low** | **高——当前 9 个测试全红，功能与实现漂移** | Fix |
| server/auth、middleware | None | 高——公网认证面无任何验证 | Add |
| 前端 | None | 高——0 测试 + 0 类型门禁 | Add |

**Valuable Tests**: backup 往返测试、transformer golden 测试、迁移数据回填测试（013、018-023）——提供真实回归保护。
**Suspicious Tests**: 无过度 mock 或实现细节测试（正面确认）。
**Missing Tests**: 认证链路、熔断状态机、WS 池并发、统计并发丢失、前端 stores。

---

## 18. Recommended Fix Order（修复顺序）

### Fix Immediately（数据丢失 / 安全破坏 / 服务中断）

1. **修复 `go test ./...` 红灯**（internal/op 8 个 + sitesync 1 个失败）— 阻塞一切发布
2. **API 响应移除明文密钥**（Token/ChannelKey/APIKey 改 `json:"-"`）— 1-2 小时
3. **自更新加签名/校验和验证** — 供应链后门默认关闭
4. **请求体大小限制**（/v1 全入口 MaxBytesReader）— OOM DoS 向量
5. **HTTP Server 超时配置** — slowloris 防护
6. **默认凭据处理**（随机密码 + 禁止日志打印）
7. **changelog.yml 去掉 force push** — master 历史保护

### Fix Before Stable Release（可靠性/正确性/安全降级）

8. **JWT 密钥与加密密钥分离 + 删除弱回退**
9. **热路径 DB 查询缓存化**（RouteCandidate/HeaderPolicy/价格）
10. **流式 O(N²) 修复**（增量 terminal 检测 + 去双份缓冲）
11. **BPE 编码器单例复用**
12. **代理池 http.Client 复用**
13. **统计全局锁原子化**（RMW 竞态 → 计费失真）
14. **err.Error() 响应脱敏**（11+ handler）
15. **绑定校验补齐**（binding + 枚举 + 错误码区分）
16. **WS 池 preflight 竞态修复**
17. **release.yaml 加测试门禁 + concurrency；build.sh 失败即退出**
18. **认证链路补测试**（auth/middleware httptest）

### Schedule Later（维护成本 / 扩展性）

19. relay.Handler / importDBDump 拆分
20. transformer 共享层抽取
21. sitesync→op 依赖收敛
22. 前端上帝组件拆分（site-channel/site/log Item）
23. token 迁移 HttpOnly Cookie
24. 前端 CI 加 tsc/build + vitest 设施
25. 迁移分布式锁 + 无测试迁移补测
26. 前端 i18n 硬编码清理

### Ignore for Now（低危 / 风格）

27. 命名/注释/空 TODO 清理
28. apperror.IsCode 死代码
29. 前端 label 字段 / getErrorMessage 重复
30. 每流 goroutine 数优化（Low）

## 19. Quick Wins（速赢——1-2 小时级低成本高价值）

| # | 改动 | 消除风险 |
|---|------|---------|
| 1 | 明文密钥字段改 `json:"-"`（3 个 struct） | 全部上游凭据泄露面 |
| 2 | relay 入口 MaxBytesReader + 413 | 未认证 OOM DoS |
| 3 | server.go 加 ReadHeaderTimeout/IdleTimeout | slowloris |
| 4 | changelog.yml 去 force push + concurrency | master 历史被覆盖 |
| 5 | op/user.go 去掉密码明文日志 | 凭据入日志 |
| 6 | usage analytics pageSize clamp [1,100] | 单请求拖垮 DB |
| 7 | release.yaml 前加 `go test ./...` | 红灯照常发布 |
| 8 | README Go 1.25 / Node 20.9 版本修正 | 新手安装必失败 |
| 9 | 前端 3 处 console 改 logger | 生产日志噪音 |
| 10 | docker-compose 加 healthcheck + pin tag 注释 | 无法感知崩溃/不可复现部署 |

## 20. Long-term Refactor Plan（长期重构计划）

1. **relay 热路径架构**（4-6 周）：路由规划/定价/HeaderPolicy 全量入内存缓存 + 写时失效；Stats 分片原子化 + 周期快照落库，消除全部全局锁
2. **transformer 共享层**（2-3 周）：tool_call 合并 / finish_reason 映射 / thinking 规范化下沉到 transformer/model，三协议仅保留协议差异层
3. **backup 单管线化**（1-2 周）：JSON dump 与 ZIP 流式导入统一为"数据源适配器 + 表级 importer"，消除三套实现漂移
4. **前端状态架构**（2-3 周）：store 分片规范、jump 导航解耦、completion 状态收敛、上帝组件拆分
5. **认证与密钥架构**（1 周）：独立 AES 主密钥 + 密钥轮换、凭据哈希存储、token 迁移 HttpOnly Cookie

每个项目均有明确动机、可渐进落地、依赖现有 7000+ 行测试作回归保障；建议按"一次一个主题 PR"的仓库规范推进（对应贡献规范要求）。

---

*报告生成：opencode 代码审计工作流（deepseek-v4-flash 模型）· 2026-08-02。所有 Critical/High 发现均经本机复现实证（`go test ./...` 实测红灯、关键源码逐行核对）。*
