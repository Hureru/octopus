# 交接说明：模型分组按需创建 + 上游名重映射 + 厂商识别

> 面向接手的 AI / 工程师。读完这份就能继续干活，不需要重新翻代码考古。
>
> 分支：`feat/model-group-provisioning`（从 `dev` 切出）
> 状态：**功能完成，全部验证通过，尚未 commit**
> 日期：2026-08-02

---

## 1. 要解决的问题

`CatalogSync()`（`internal/op/catalog.go:83`）会把所有渠道的模型名全量展开，**给每个名字无条件创建 `CanonicalModel` 和同名 `Group`**。触发点有三处：

- 启动时：`internal/op/cache.go:35`（`InitCache`）
- 站点同步后：`internal/sitesync/core.go:62`
- 手动：`POST /api/v1/model/catalog/sync`

接一个聚合站（上游几百个模型）之后，分组页会瞬间冒出几百个分组，无法使用。

用户的三个诉求：

1. 先列出所有 model，再选择哪些建分组
2. 能把一个或多个 model 重映射到某个分组（例：`z-ai/glm-5.2` → `glm-5.2`，本来就是同一个模型）
3. 给每个 model 匹配厂商（类似 metapi 的展示），便于筛选和分类

---

## 2. 关键前提：为什么改动比想象中小

动手前先确认了两件事，决定了整体方案：

**（a）重映射所需的数据模型和路由链路已经存在。**
`ModelAlias`（`internal/model/routing.go:160`）本来就能表达 `z-ai/glm-5.2 → canonical glm-5.2`；`CatalogSync` 已经会走别名解析；relay 也已按别名折算路由名（`internal/relay/relay.go:70-76`）。缺的只是「自动建组之前先让人选」，以及「存量 canonical 会顶掉别名」——`CatalogAliasUpsert` 遇到同名 canonical 会报 `alias conflicts with canonical model`（`internal/op/catalog.go:553`）。

参见 ADR `docs/adr/0010-separate-canonical-models-from-route-candidates.md`：Canonical Model 是对客户端稳定的身份，Route Candidate 是「站点 + 账号 + 分组 + 渠道 + 协议 + 上游模型名」的具体组合。本次改动完全遵循这条分层，没有引入新的路由概念。

**（b）路由完全由分组名驱动。**
`GroupGetEnabledMap(name)`（`internal/op/group.go:41`）按分组名取路由目标，`/v1/models` 只列分组名（`GroupListModel`，`group.go:25`）。所以：

> **不建 canonical/group ⇒ 该模型不对外提供、`/v1/models` 里不出现、请求它返回 404。**

这正是「按需建组」想要的语义，不需要额外做「屏蔽/下架」机制。

---

## 3. 改动总览

### 3.1 后端

| 文件 | 性质 | 作用 |
|------|------|------|
| `internal/modelvendor/rules.go` | 新增 | 22 个厂商常量 + `vendor/model` 前缀别名表 + 命名规则表 |
| `internal/modelvendor/vendor.go` | 新增 | `Detect(name) string`，短 token 边界判定 |
| `internal/modelvendor/index.go` | 新增 | models.dev 注册表索引，`ReplaceIndex` / `IndexSize` |
| `internal/modelvendor/vendor_test.go` | 新增 | 表驱动测试 |
| `internal/price/price.go` | 改 | 复用已有的 models.dev 请求，额外产出「模型名 → provider」索引喂给 `modelvendor` |
| `internal/model/routing.go` | 改 | `CanonicalModel` 加 `Vendor` / `VendorManual`；`CatalogSyncResult` 加 `Skipped` |
| `internal/model/catalog_provisioning.go` | 新增 | 供给策略枚举、`DiscoveredModel`、provision/unprovision 的请求与结果类型 |
| `internal/model/setting.go` | 改 | 新设置 key + 默认值 + 校验 |
| `internal/db/migrate/023.go` | 新增 | 加 `vendor` / `vendor_manual` 列，并回填存量 canonical 的厂商 |
| `internal/op/catalog_provisioning.go` | 新增 | `CatalogGroupProvisioningMode()` 读设置，缺失时回退 `DefaultSettings` |
| `internal/op/catalog.go` | 改 | **核心**：manual 模式下不凭空建 canonical/group；新建时写入厂商；抽出接线逻辑 |
| `internal/op/catalog_wiring.go` | 新增 | `catalogEnsureWiring`，从 `CatalogSync` 抽出的「补 group item + route candidate」 |
| `internal/op/catalog_discovery.go` | 新增 | `CatalogDiscoveredModels`，聚合出发现列表 |
| `internal/op/catalog_provision.go` | 新增 | `CatalogProvision` / `CatalogUnprovision` 主流程 |
| `internal/op/catalog_provision_tx.go` | 新增 | 事务内的 ensure/cleanup 辅助 |
| `internal/op/catalog_merge_tx.go` | 新增 | 事务内的合并/别名辅助 |
| `internal/op/catalog_provision_errors.go` | 新增 | `apperror` 风格的错误构造 |
| `internal/op/setting.go` | 改 | 导出 `SettingRefreshCache`（照 `GroupRefreshCacheByIDs` 的既有惯例），供跨包测试初始化设置 |
| `internal/server/handlers/model.go` | 改 | 3 个新路由 |

### 3.2 前端

| 文件 | 性质 | 作用 |
|------|------|------|
| `web/src/api/endpoints/model-catalog.ts` | 改 | 新类型 + `useDiscoveredModels` / `useProvisionModels` / `useUnprovisionModels` |
| `web/src/api/endpoints/setting.ts` | 改 | `SettingKey.CatalogGroupProvisioning` |
| `web/src/components/modules/model/index.tsx` | 改 | 新增「模型发现」Tab |
| `web/src/components/modules/model/Discovery.tsx` | 新增 | 主面板：筛选、多选、批量操作编排 |
| `web/src/components/modules/model/DiscoveryToolbar.tsx` | 新增 | 顶部工具条 + 批量操作条 |
| `web/src/components/modules/model/DiscoveryRow.tsx` | 新增 | 列表行（固定行高，避免筛选切换抖动） |
| `web/src/components/modules/model/DiscoveryDialogs.tsx` | 新增 | 映射对话框 + 取消分组二次确认 |
| `web/src/components/modules/model/VendorBadge.tsx` | 新增 | 厂商标签 |
| `web/src/components/modules/model/vendor-options.ts` | 新增 | 厂商 id → 展示名/配色，与后端常量对齐 |
| `web/src/components/modules/model/Catalog.tsx` | 改 | 目录列表也带上厂商标签 |
| `web/public/locale/{en,zh_hans,zh_hant}.json` | 改 | `model.workspace.discovery` + `model.discovery.*`（47 个 key × 3 语言） |

---

## 4. 行为契约

### 4.1 新设置

```
key:     catalog_group_provisioning
values:  "manual"（默认） | "auto"
```

- **manual**：`CatalogSync` 只为「已经存在同名分组」的模型建立目录条目，其余计入 `result.Skipped`（按模型去重，不按渠道数放大）。**永不凭空建分组。**
- **auto**：完全等同改动前的行为，一行逻辑没变，用于兼容和「先铺开再收拢」的用法。

存量库不需要迁移设置：`settingRefreshCache`（`internal/op/setting.go`）本来就会补齐缺失的默认设置。

> ⚠️ **这是一次默认行为翻转。** 升级后不再自动建组，已有分组不受影响，但新出现的上游模型需要手动纳入。

### 4.2 三个新接口

```
GET  /api/v1/model/catalog/discovered
POST /api/v1/model/catalog/provision
POST /api/v1/model/catalog/unprovision
```

**discovered** → `[]DiscoveredModel`，每行一个上游模型（多渠道合并），字段见 `internal/model/catalog_provisioning.go`。状态三态：

- `ungrouped`：不在目录里，请求它 404
- `grouped`：以自身名字建了 canonical + 分组
- `mapped`：作为别名映射到了别的 canonical

排序：按厂商，未知厂商排最后，同厂商内按归一化名。

**provision**：

```jsonc
{
  "models": ["z-ai/glm-5.2"],           // 必填
  "target_name": "glm-5.2",             // 空 = 每个模型各建同名分组
  "delete_empty_source_groups": true    // 合并后删除已空的原分组
}
```

`target_name` 非空且与模型名不同时走**重映射**：

1. 目标 canonical / group 不存在则创建（默认值与自动建组一致：`GroupModeRoundRobin`、`MaxRetries: 3`）
2. 模型名若已被自动建成独立 canonical → **合并**：路由候选改挂目标（撞唯一索引 `idx_route_candidate_identity` 的重复项删除）、别名迁移、删源 canonical
3. 写入 `模型名 → 目标 canonical` 的别名（已指向别处则改指向，保证可重复执行）
4. 对所有提供该模型的渠道补 group item + route candidate，**上游名原样保留在 group item 上**
5. 原分组若只剩被映射走的模型，且开关打开 → 删除

**unprovision**：

```jsonc
{ "models": ["z-ai/glm-5.2"], "delete_group": true }
```

无论模型是自建分组还是别名映射，**引用它的分组条目与路由候选都会一并清除**——只删别名的话该模型仍会被目标分组继续路由，状态显示与实际行为会打架。`delete_group` 只控制是否额外删掉与模型同名的分组。

### 4.3 厂商识别

`modelvendor.Detect(name)` 顺序：

1. `vendor/model` 前缀（逐段查 `prefixAliases`，`z-ai|zhipu|glm→zhipuai`、`qwen→alibaba`、`meta-llama→meta` …）；未收录的段（`openrouter` 等托管方）被忽略，继续往下走
2. 去掉厂商段后的名称模式表（`claude-*`、`gpt-*|o[1-4]-*`、`gemini-*|gemma-*`、`doubao-*` …）
3. models.dev 索引
4. 空串（未知）

**短 token 边界规则**：长度 ≤ `shortTokenMaxLen`(4) 的 token 命中后要求紧随字符不是字母，否则 `yi` 会匹配到 `yielding-model`。改规则表时注意别破坏这条。

**models.dev 索引只采信能映射到已知厂商的 provider**（`index.go` 的 `ReplaceIndex`）。models.dev 里 `openrouter`/`groq` 这类托管方会把同一个模型登记在自己名下，直接采信会让厂商归属随 map 遍历顺序漂移。代价是新厂商不会自动出现，需要往 `prefixAliases` 里加。

---

## 5. 实现中几个「刻意为之」的点

改之前请先看这几条，都是踩过或想清楚过的：

1. **`deleteRedundantGroupsTx` 刻意不走 `GroupDel`**（`internal/op/catalog_provision_tx.go`）。`GroupDel` 会调 `ensureRouteCandidatesForGroupTx`，把该分组名解析出的 canonical 下所有候选标记为 `unavailable`。重映射场景里这些候选刚被移交给目标模型，退役它们等于把刚接好的路由拆掉。而 `CatalogUnprovision` 的删分组**是**走 `GroupDel` 的——那里退役语义正确。

2. **`CatalogSync` 里 canonical/group 的注册表从「切片 + 索引指针」改成「map + 堆指针」**。原写法 `canonicals = append(...); canonical = &canonicals[len-1]` 在切片扩容后会让 map 里的旧指针指向废弃数组。原代码只读 `.ID`/`.Name` 所以没爆，但本次要往 canonical 上写 `Vendor`，必须先修掉。顺带把别名解析的 O(n) 线性扫描换成 `canonicalByRecordID` 查表。

3. **`findGroupByNameTx` 用 `LOWER(name) = ?`**。分组名大小写由用户决定，目录侧一律按归一化小写比较，`CatalogSync` 的 `groupByNormalized` 也是这个口径。别改回 `name = ?`。

4. **`result.Skipped` 按模型去重**，不是按「渠道 × 模型」，否则前端「N 个模型未选中」的文案会虚高。

5. **`internal/modelvendor` 不能叫 `internal/vendor`** —— Go 会把任意 `vendor/` 目录当作 vendoring 目录。

6. **依赖方向**：`price → modelvendor`、`op → modelvendor`。`modelvendor` 不能 import `op`/`price`（`price` 已经 import `op`，反向会成环）。

---

## 6. 测试与验证

### 6.1 环境坑（重要）

```bash
# go 不在默认 PATH 上
export PATH="/usr/local/bin:$PATH"   # go1.26.5 darwin/amd64

# pnpm 不在 PATH 上，走 corepack
cd web && corepack pnpm lint
```

### 6.2 命令与结果

```bash
export PATH="/usr/local/bin:$PATH"
go build ./...                    # OK
go vet ./internal/...             # OK
go test ./... -count=1 -timeout 400s   # 全绿（internal/op 连跑 3 次稳定）

cd web
corepack pnpm lint                # 0 error 0 warning
corepack pnpm build               # 含 TypeScript 检查，通过
```

### 6.3 新增测试

- `internal/modelvendor/vendor_test.go`：前缀/模式/短 token 边界/注册表增强/本地规则优先级
- `internal/op/catalog_provision_test.go`：manual 跳过、manual 仍服务已有分组、逐个建组、重映射合并（含候选是「迁移」而非「重建」的断言）、原分组含其它模型时保留、取消分组、发现列表状态与厂商、**unprovision canonical 头清理别名遗留分组条目**（`TestCatalogUnprovisionCanonicalHeadCleansAliasGroupItems`）

### 6.4 既有测试的改动（不是 bug，是默认翻转的必然结果）

- `internal/op/catalog_test.go`：8 个依赖自动建组的用例加了 `useAutoCatalogProvisioning(t)` 显式开关（helper 在该文件顶部，带 `t.Cleanup`）
- `internal/sitesync/pricing_test.go`：`TestSyncAccountBindsFirstPricingRefreshToProjectedCandidate` 显式启用 auto —— 它验证的是「有候选时价格必须绑定」，不是供给策略

### 6.4.1 全面 review 后的修复（2026-08-02 追加）

上一轮全面 review 发现并修复了 3 个问题，其中 2 个针对本次新增的供给/接线逻辑：

- **问题 #1（已修复）`CatalogUnprovision` 只删 canonical 头，别名叶子的分组条目成为死路由。**
  当 `z-ai/glm-5.2 → glm-5.2` 这种映射存在、且只 unprovision 而不带 `delete_group` 时，canonical 头被删后，目标分组里仍残留 `ModelName = z-ai/glm-5.2` 的 group item 与 route candidate（指向已删除的 canonical）。修复：`catalog_provision.go` 在删除 canonical 前，遍历指向它的别名，逐个调用 `removeGroupItemsByModelTx` 清理其分组条目与受影响渠道。回归测试：`TestCatalogUnprovisionCanonicalHeadCleansAliasGroupItems`（断言 `group.Items` 为空、`z-ai/glm-5.2` 的路由候选数为 0）。

- **`catalogEnsureWiring` 重复供给时把路由候选的优先级/权重覆盖成 0**：`CatalogProvision` 不携带 priority/weight，原写法在「分组条目已存在」分支直接落 0。修复：`catalog_wiring.go` 在分组条目已存在时以既有条目的 `Priority`/`Weight` 为基线（`priority <= 0` / `weight <= 0` 才回退），新建条目时保持原逻辑（`len(group.Items)+1` / `1`）。两条调用路径（`CatalogSync` 与 `CatalogProvision`）都 Preload 了 `Items`，行为一致。

- **`.gitignore` 缺少 `.DS_Store`**：补上（与本次功能无关，顺手处理仓库卫生）。

### 6.5 ⚠️ `internal/op` 的测试隔离缺陷

`setupBackupTestDB(t)` **只重建 SQLite 文件，不清理包级缓存**（`channelCache` / `settingCache` / `groupCache`）。上一个用例创建的渠道会带进下一个用例，任何依赖「当前有哪些渠道」的断言都会随执行顺序漂移——**单跑 PASS、整包跑 FAIL**。

新写目录/供给类测试请用 `internal/op/catalog_provision_test.go` 里的 `setupCatalogProvisionTest(t)`（在 `setupBackupTestDB` 之上重置 `channelCache`，并在 `t.Cleanup` 再清一次）。这是本次实际踩到的坑。

### 6.6 端到端（已跑通，用临时 DB + 临时端口，未触碰仓库的 `data/`）

```bash
# 起后端
mkdir -p /tmp/octopus-e2e
cat > /tmp/octopus-e2e/config.json <<'JSON'
{"server":{"host":"127.0.0.1","port":18080},
 "database":{"type":"sqlite","path":"/tmp/octopus-e2e/data.db"},
 "log":{"level":"warn","format":"console"}}
JSON
export PATH="/usr/local/bin:$PATH"
go run main.go start --config /tmp/octopus-e2e/config.json

# 起前端（跨域，需要先把后端 cors_allow_origins 设成 *）
cd web && NEXT_PUBLIC_API_BASE_URL="http://127.0.0.1:18080" corepack pnpm dev
```

默认账号 `admin/admin`。验证过的链路：

1. manual 同步 → 建 0 个分组，`skipped=4`
2. 发现列表厂商识别全对（zhipuai / anthropic / openai / alibaba / deepseek / 未知）
3. provision 建组 → 分组与条目正确
4. remap `z-ai/glm-5.2 → glm-5.2` → 别名建立、候选迁移不重复、上游名保留
5. `/v1/models` 只暴露选中的分组名
6. 用 `z-ai/glm-5.2` 发请求 → 502（上游 example.com 不可达，说明路由命中了），未纳入的模型 → 404 `relay.model_not_found`
7. 切 auto → 同步 → 复现「历史自动建组」状态 → 切回 manual → 合并进目标分组并删除冗余原组
8. Playwright 走完 UI：筛 GLM（2/7）→ 全选 → 映射到 `glm-5.2` → 两行变「已映射」，后端分组含两条上游条目

---

## 7. 已知遗留 / 不在本次范围

1. **站点价格绑定有延迟。** manual 模式下未选中的模型没有 route candidate，`linkPriceQuoteRouteCandidate`（`internal/op/site_pricing.go:172`）绑不上。该函数在每次报价 upsert 时都会对未绑定的报价重试，所以 provision 之后**下一次站点同步或价格刷新会自动补上**（默认间隔 12h）——机制自愈，但不是即时的。想做成即时，需要在 `CatalogProvision` 之后重跑受影响报价的 normalize+upsert（注意 `RefreshIdentityKey` 与 `deleteSupersededUnboundQuote` 的配合，不能简单 UPDATE）。

2. **`internal/sitesync/` 逻辑没动。** 站点同步完照常调 `CatalogSync`，因此同样受新设置约束。

3. **没有为这次决策写 ADR。** `docs/adr/` 有既有惯例，如果认为「默认不自动建组」值得固化为架构决策，可以补一篇 0011。

4. **厂商规则表是本地维护的。** 新厂商需要往 `internal/modelvendor/rules.go` 的 `prefixAliases` 和 `namePatterns` 加，前端 `vendor-options.ts` 补展示名/配色（不加也能跑，会回落成中性样式 + 原始 id）。

5. **`CanonicalModel.Vendor` 支持人工覆盖**（`CatalogCanonicalUpdate` 接受 `vendor`，非空时置 `VendorManual=true`，之后不再被自动识别覆盖），但**前端还没做这个编辑入口**。接口是通的。

---

## 8. 工作区状态（接手前必读）

- 分支 `feat/model-group-provisioning`，**改动未 commit**。
- **这个工作区是共享的**：另有一个任务在做 Cloudflare 绕过，它改了 `extensions/verification-bridge/manifest.json`、`extensions/verification-bridge/popup.js`，并新增了 `audit-report-octopus-2026-08-02-deepseek.md`。这些**不属于本次改动**，commit 时不要带上。

本次改动的文件清单（15 个修改 + 20 个新增，不含本交接文档）：

```
# 改
internal/model/routing.go
internal/model/setting.go
internal/op/catalog.go
internal/op/catalog_test.go
internal/op/setting.go
internal/price/price.go
internal/server/handlers/model.go
internal/sitesync/pricing_test.go
web/public/locale/en.json
web/public/locale/zh_hans.json
web/public/locale/zh_hant.json
web/src/api/endpoints/model-catalog.ts
web/src/api/endpoints/setting.ts
web/src/components/modules/model/Catalog.tsx
web/src/components/modules/model/index.tsx

# 新增
internal/db/migrate/023.go
internal/model/catalog_provisioning.go
internal/modelvendor/{rules,vendor,index,vendor_test}.go
internal/op/catalog_discovery.go
internal/op/catalog_merge_tx.go
internal/op/catalog_provision.go
internal/op/catalog_provision_errors.go
internal/op/catalog_provision_test.go
internal/op/catalog_provision_tx.go
internal/op/catalog_provisioning.go
internal/op/catalog_wiring.go
web/src/components/modules/model/Discovery.tsx
web/src/components/modules/model/DiscoveryDialogs.tsx
web/src/components/modules/model/DiscoveryRow.tsx
web/src/components/modules/model/DiscoveryToolbar.tsx
web/src/components/modules/model/VendorBadge.tsx
web/src/components/modules/model/vendor-options.ts
```

按 `CONTRIBUTING.md`「每个 PR 只包含一个变更主题」，这些应作为一个主题一起提交，与 CF 绕过的改动分开。
