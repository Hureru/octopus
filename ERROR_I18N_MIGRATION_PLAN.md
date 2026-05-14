# 错误码与错误国际化迁移计划

## 目标

将当前“后端直接返回自然语言错误字符串、前端按字符串/正则翻译”的模式，逐步迁移为：

```json
{
  "code": 400,
  "error_code": "site.sync.missing_group_key",
  "message": "default fallback message"
}
```

其中：

- `code`：HTTP 状态码。
- `error_code`：稳定机器可读错误码，供前端翻译和业务判断。
- `message`：默认兜底文案，供日志、未翻译场景和兼容旧客户端使用。

## 当前基础状态

已完成基础框架：

- [x] 后端新增 `internal/apperror`。
- [x] 后端响应结构支持 `error_code`。
- [x] 后端新增 `resp.ErrorWithCode` / `resp.ErrorWithAppError`。
- [x] 前端 API client 支持按 `error_code` 自动翻译。
- [x] 前端新增 `web/src/api/error-i18n.ts`。
- [x] 三种语言 locale 已新增 `errors.*` 根节点。
- [x] Sub2API 同步相关错误码已接入。
- [x] 已提交基础框架 commit：`a68edf5 feat: :globe_with_meridians: add coded API errors for Sub2API sync`。

## 约定

### 错误码命名规范

使用点分层级：

```text
<domain>.<module>.<reason>
```

示例：

```text
common.invalid_json
site.sync.missing_group_key
site.sub2api.api_key_required
auth.invalid_credentials
relay.no_available_channel
```

命名边界：

- `common.*`：跨模块通用请求、参数、校验、资源和服务端错误。
- `auth.*`：Octopus 后台自身登录、Token、权限、API Key 认证错误。
- `site.auth.*`：第三方站点账号、站点 Access Token、Direct Token、上游登录失败。
- `site.upstream.*`：第三方站点 HTTP、响应格式、Cloudflare 等上游交互错误。
- `relay.*`：Relay 请求路由、兼容协议、上游模型调用相关错误。

### 后端迁移原则

1. 新增错误优先用 `apperror.New` / `apperror.Newf` / `apperror.Wrap`。
2. Handler 返回错误时优先用 `resp.ErrorWithAppError`。
3. 不要求一次性迁移全部旧错误。
4. 每迁移一组错误，必须同步补 locale。
5. `message` 保持可读，不能只返回错误码。
6. 如果错误包含变量，应优先放入响应 `params` 供前端插值，同时在默认 `message` 保留完整可读信息。
7. `apperror` 可以携带建议 HTTP status；handler 使用 `resp.ErrorWithAppError` 时应优先采用错误自身 status，未设置时使用 fallback status。
8. 上游站点认证失败不应直接返回后台 API 的 HTTP 401，避免误触发前端后台 logout；仅 Octopus 自身认证失效使用 HTTP 401。

### 前端迁移原则

1. API client 自动翻译 `error_code`，并读取响应 `params` 做插值。
2. 错误翻译以 `web/public/locale/{zh_hans,zh_hant,en}.json` 的 `errors.*` 为单一来源，避免在 `error-i18n.ts` 维护第二份文案。
3. 组件里优先显示 `error.message`。
4. 旧的字符串匹配翻译逻辑暂时保留，作为兼容层。
5. 等对应后端错误全部迁移后，再删除前端正则匹配。

## 迁移进度总览

| 阶段 | 范围 | 状态 | 优先级 |
|---|---|---:|---:|
| 0 | 基础框架 + Sub2API 同步错误 | 已完成 | P0 |
| 0.5 | 错误框架加固：locale 单一来源、params、HTTP status、通用错误基础能力 | 已完成 | P0 |
| 1 | 站点同步核心错误 | 已完成 | P0 |
| 2 | 站点导入错误 | 已完成 | P0 |
| 3 | 通用请求/校验错误 | 已完成 | P1 |
| 4 | 认证与权限错误 | 已完成 | P1 |
| 5 | 站点渠道管理错误 | 已完成 | P1 |
| 6 | Channel / Group / Model CRUD 错误 | 已完成 | P2 |
| 7 | Relay 运行时错误 | 已完成 | P2 |
| 8 | 前端旧字符串匹配清理 | 已完成 | P3 |

---

# 阶段 0：基础框架 + Sub2API 同步错误

## 状态

- [x] 已完成。

## 已接入错误码

```text
site.sub2api.api_key_required
site.sub2api.model_api_key_required
site.sub2api.envelope_failed
site.sub2api.missing_data
```

## 已改文件

后端：

- `internal/apperror/apperror.go`
- `internal/server/resp/resp.go`
- `internal/server/handlers/site.go`
- `internal/sitesync/sync.go`
- `internal/sitesync/sync_fetch.go`
- `internal/sitesync/balance.go`
- `internal/sitesync/create_key.go`
- `internal/sitesync/sub2api_auth.go`

前端：

- `web/src/api/error-i18n.ts`
- `web/src/api/client.ts`
- `web/src/api/types.ts`
- `web/public/locale/zh_hans.json`
- `web/public/locale/zh_hant.json`
- `web/public/locale/en.json`

测试：

- `internal/sitesync/sub2api_test.go`

## 验证命令

```bash
go test ./...
cd web && pnpm lint
```

当前验证结果：

- `go test ./...` 通过。
- `pnpm lint` 无 error，有既有 warning：`AllAPIHubImportResult` 未使用。

---

# 阶段 0.5：错误框架加固

## 目标

在继续迁移更多业务错误前，先补齐错误国际化基础能力，避免后续阶段重复返工。

重点解决：

1. 前端错误翻译来源统一，避免 `error-i18n.ts` 和 locale JSON 双写。
2. 后端错误支持 `params`，前端可对错误文案做变量插值。
3. `apperror` 支持建议 HTTP status，避免所有业务错误都返回 500。
4. 通用错误码具备最小可用能力，为后续 handler 批量迁移打基础。
5. 明确上游站点错误和 Octopus 自身认证错误的 HTTP status 边界。

## 响应结构补充

目标错误响应：

```json
{
  "code": 400,
  "error_code": "site.sync.missing_group_key",
  "message": "site sync requires a key for group \"default\"; create a key for that group on the site and sync again",
  "params": {
    "groupKey": "default"
  }
}
```

字段说明：

- `code`：HTTP 状态码。
- `error_code`：稳定机器可读错误码。
- `message`：默认 fallback 文案，必须可读。
- `params`：可选，供前端 i18n 插值使用；不放敏感信息。

## HTTP status 约定

`apperror` 可携带建议 status；`resp.ErrorWithAppError(c, fallbackStatus, err)` 应按以下顺序选择响应状态：

1. `apperror.Status(err)` 非 0 时使用错误自身 status。
2. 否则使用调用方传入的 `fallbackStatus`。

初始建议映射：

```text
common.invalid_json                  -> 400
common.invalid_param                 -> 400
common.validation_failed             -> 400
common.bad_request                   -> 400
common.not_found                     -> 404
common.duplicate_resource            -> 409
common.database_error                -> 500
common.internal_error                -> 500
site.sync.unsupported_platform       -> 400
site.sync.missing_group_key          -> 400
site.sync.no_group_result            -> 500
site.sync.all_groups_unresolved      -> 502
site.auth.access_token_required      -> 400
site.auth.direct_token_required      -> 400
site.auth.login_failed               -> 502
site.auth.login_token_missing        -> 502
site.upstream.http_error             -> 502
site.upstream.decode_failed          -> 502
site.upstream.cloudflare_challenge   -> 503
auth.unauthorized                    -> 401
auth.forbidden                       -> 403
```

注意：第三方站点的 401/403 不直接作为后台 API HTTP 401/403 返回，避免前端误认为 Octopus 登录失效并执行 logout。

## 前端 locale 单一来源

当前 `web/src/api/error-i18n.ts` 内部硬编码了错误翻译表，同时 `web/public/locale/*.json` 也有 `errors.*`。阶段 0.5 需要改成：

- `web/public/locale/{zh_hans,zh_hant,en}.json` 的 `errors.*` 是唯一文案来源。
- `error-i18n.ts` 只负责：
  - 根据当前 locale 选择 locale JSON。
  - 按 `error_code` 点路径查找 `errors.*`。
  - fallback 顺序：当前语言 -> 简体中文 -> 英文 -> 后端 `message`。
  - 对 `params` 做 `{name}` 插值。

## 后端实现任务

- [x] 扩展 `internal/apperror.Error`：支持 `Status int` 和 `Params map[string]any`。
- [x] 增加构造/链式方法，例如 `WithStatus`、`WithParams`、`Status(err)`、`Params(err)`。
- [x] 定义通用错误码常量：`common.invalid_json`、`common.invalid_param`、`common.validation_failed`、`common.bad_request`、`common.not_found`、`common.duplicate_resource`、`common.database_error`、`common.internal_error`。
- [x] 扩展 `internal/server/resp.ResponseStruct`：增加 `params,omitempty`。
- [x] 修改 `resp.ErrorWithAppError`：使用 `apperror.Status(err)` 覆盖 fallback status，并输出 `params`。
- [x] 增加便捷响应函数或常量映射：`InvalidJSON`、`InvalidParam` 等，后续 handler 可逐步使用。
- [x] 增加单元测试覆盖：code/message/status/params、wrapped error、fallback status。

## 前端实现任务

- [x] 修改 `web/src/api/error-i18n.ts`，从 locale JSON 的 `errors` 节点读取翻译。
- [x] 修改 `web/src/api/types.ts`，给 `ApiResponse` / `ApiError` 增加 `params` 类型。
- [x] 修改 `web/src/api/client.ts`，解析响应 `params` 并传给 `translateApiErrorCode`。
- [x] 确认 locale JSON 中已存在 `errors.site.sub2api.*`，并补充 `common.*` 基础文案。
- [x] 保留旧 `site-message.ts` 字符串匹配兼容逻辑，不在本阶段清理。

## 验收标准

- [x] 后端返回带 `params` 的 coded error 时，JSON 响应包含 `params`。
- [x] `resp.ErrorWithAppError` 能使用 app error 自带 HTTP status。
- [x] 前端 API client 能从 locale JSON 翻译 `error_code`。
- [x] 前端能用 `params` 插值错误文案。
- [x] 未翻译错误码 fallback 到后端 `message`。
- [x] `go test ./...` 通过。
- [x] `cd web && pnpm lint` 无 error。

---

# 阶段 1：站点同步核心错误

## 目标

迁移 `internal/sitesync` 中用户最常遇到的同步错误，减少前端对英文字符串和正则的依赖。

## 建议错误码

```text
site.sync.missing_group_key
site.sync.group_models_unresolved
site.sync.no_group_result
site.sync.all_groups_unresolved
site.sync.unsupported_platform
site.sync.snapshot_nil
site.auth.access_token_required
site.auth.direct_token_required
site.auth.login_failed
site.auth.login_token_missing
site.upstream.http_error
site.upstream.decode_failed
site.upstream.cloudflare_challenge
```

## 主要涉及文件

- `internal/sitesync/sync.go`
- `internal/sitesync/sync_result.go`
- `internal/sitesync/storage.go`
- `internal/sitesync/http.go`
- `internal/sitesync/errors.go`
- `internal/server/handlers/site.go`
- `web/public/locale/zh_hans.json`
- `web/public/locale/zh_hant.json`
- `web/public/locale/en.json`
- `web/src/components/modules/site/site-message.ts`

## 任务清单

基础要求：

- [x] 每个错误码明确 HTTP status。
- [x] 包含变量的错误使用 `params`，例如 `groupKey`、`platform`、`statusCode`。
- [x] 上游站点 401/403 不返回后台 API HTTP 401/403，避免触发前端 logout。
- [x] Cloudflare 错误迁移时保留 `CloudflareProtectionError` 的 `errors.As` / `IsCloudflareProtectionError` 能力。

具体迁移：

- [x] 将 `site sync requires a key for group ...` 改为 `site.sync.missing_group_key`。
- [x] 将 `site sync could not resolve models for group ...` 改为 `site.sync.group_models_unresolved`（当前代码无该旧字符串，已预留错误码与 locale）。
- [x] 将 `站点账号同步失败：没有可用的分组同步结果` 改为 `site.sync.no_group_result`。
- [x] 将 `所有分组都未能确认模型` 改为 `site.sync.all_groups_unresolved`。
- [x] 将 `unsupported site platform` 改为 `site.sync.unsupported_platform`。
- [x] 将 `access token is required` 改为 `site.auth.access_token_required`。
- [x] 将 `direct token is required` 改为 `site.auth.direct_token_required`。
- [x] 将登录失败类错误改为 `site.auth.login_failed` / `site.auth.login_token_missing`。
- [x] 给 HTTP 非 2xx 错误建立 `site.upstream.http_error`。
- [x] 给 JSON decode 错误建立 `site.upstream.decode_failed`。
- [x] Cloudflare 防护错误接入 `site.upstream.cloudflare_challenge`。
- [x] 给以上错误补三语 locale。
- [x] 为核心错误补 Go 单元测试。
- [x] 测试 `apperror.Code(err)` 对 wrapped error 生效。
- [x] 测试 Cloudflare 错误同时满足 `error_code=site.upstream.cloudflare_challenge` 和 `IsCloudflareProtectionError(err)`。
- [x] 前端 `site-message.ts` 对已迁移错误保留兼容，但不再新增正则。

## 验收标准

- [x] 手动同步账号时，核心同步错误响应包含 `error_code`。
- [x] 前端 toast 展示本地化文案。
- [x] 旧 message fallback 仍可用。
- [x] `go test ./...` 通过。
- [x] `cd web && pnpm lint` 无 error。

---

# 阶段 2：站点导入错误

## 目标

迁移当前前端已通过 `site-message.ts` 做字符串映射的导入错误。

## 建议错误码

```text
site.import.invalid_json
site.import.empty_payload
site.import.unrecognized_all_api_hub
site.import.unrecognized_metapi
site.import.no_importable_all_api_hub
site.import.no_importable_metapi
site.import.unsupported_payload
site.import.persist_failed
```

## 主要涉及文件

- `internal/op/site_import.go`
- `internal/server/handlers/site.go`
- `web/src/components/modules/site/site-message.ts`
- `web/public/locale/zh_hans.json`
- `web/public/locale/zh_hant.json`
- `web/public/locale/en.json`

## 任务清单

- [x] 梳理 `site_import.go` 当前返回的固定错误字符串。
- [x] 为固定错误定义 `apperror` 错误码。
- [x] 修改导入 handler 使用 `resp.ErrorWithAppError`。
- [x] 补三语 locale。
- [x] 更新或新增导入相关测试。
- [x] 标记 `site-message.ts` 中对应 exact message 映射为兼容逻辑。

## 验收标准

- [x] All API Hub 导入错误返回 `site.import.*`。
- [x] Metapi 导入错误返回 `site.import.*`。
- [x] 前端能按当前语言显示翻译。
- [x] 旧字符串映射仍不破坏。

---

# 阶段 3：通用请求/校验错误

## 目标

迁移高频通用错误，如 JSON 错误、参数错误、校验错误、资源不存在等。

## 建议错误码

```text
common.invalid_json
common.invalid_param
common.validation_failed
common.bad_request
common.not_found
common.duplicate_resource
common.database_error
common.internal_error
```

## 主要涉及文件

- `internal/server/resp/error.go`
- `internal/server/resp/resp.go`
- `internal/server/handlers/*.go`
- `web/public/locale/*.json`

## 任务清单

- [x] 在 `apperror` 或 `resp` 中定义通用错误码常量。
- [x] 把 `resp.ErrInvalidJSON` 配套错误码 `common.invalid_json`。
- [x] 把 `resp.ErrInvalidParam` 配套错误码 `common.invalid_param`。
- [x] 批量迁移 handler 中的 `resp.Error(c, ..., resp.ErrInvalidJSON)`。
- [x] 批量迁移 handler 中的 `resp.Error(c, ..., resp.ErrInvalidParam)`。
- [x] 对 validation 错误保留原 message，附加 `common.validation_failed`。
- [x] 补三语 locale。

## 验收标准

- [x] 提交 JSON 格式错误时有 `error_code=common.invalid_json`。
- [x] 参数错误时有 `error_code=common.invalid_param`。
- [x] 前端展示本地化文案。

---

# 阶段 4：认证与权限错误

## 目标

迁移登录、API Key、Token、权限相关错误。

## 建议错误码

```text
auth.unauthorized
auth.forbidden
auth.invalid_token
auth.expired_token
auth.invalid_credentials
auth.api_key_expired
auth.api_key_missing
auth.password_incorrect
```

## 主要涉及文件

- `internal/server/middleware/auth.go`
- `internal/server/auth/*`
- `internal/op/user.go`
- `internal/server/handlers/user.go`
- `web/public/locale/*.json`

## 任务清单

- [x] 梳理 auth middleware 所有 `resp.Error`。
- [x] 登录失败返回 `auth.invalid_credentials`。
- [x] token 缺失返回 `auth.unauthorized` 或 `auth.invalid_token`。
- [x] API Key 过期返回 `auth.api_key_expired`。
- [x] 密码错误返回 `auth.password_incorrect`。
- [x] 补三语 locale。
- [x] 验证登录页和 API Key Dashboard 行为。

## 验收标准

- [x] 登录失败本地化。
- [x] API Key 过期本地化。
- [x] 未登录/Token 失效仍能触发 logout。

---

# 阶段 5：站点渠道管理错误

## 目标

迁移 `site-channel` 中模型路由、禁用、key 管理、投影相关错误。

## 建议错误码

```text
site_channel.account_not_found
site_channel.site_not_found
site_channel.model_not_found
site_channel.route_update_failed
site_channel.model_disable_failed
site_channel.key_create_failed
site_channel.source_key_update_failed
site_channel.project_failed
```

## 主要涉及文件

- `internal/server/handlers/site_channel.go`
- `internal/op/site_channel.go`
- `internal/sitesync/create_key.go`
- `web/src/components/modules/site-channel/*`
- `web/public/locale/*.json`

## 任务清单

- [x] 路由更新失败接入错误码。
- [x] 模型禁用失败接入错误码。
- [x] 快速创建 key 失败接入错误码。
- [x] 投影失败接入错误码。
- [x] 前端站点渠道页面统一显示 `error.message`。

## 验收标准

- [x] 站点渠道页错误本地化。
- [x] 路由 / 禁用 / 创建 key 操作失败能显示明确翻译。

---

# 阶段 6：Channel / Group / Model CRUD 错误

## 目标

迁移通道、分组、价格模型配置等普通管理错误。

## 建议错误码

```text
channel.not_found
channel.create_failed
channel.update_failed
channel.delete_failed
channel.fetch_models_failed
group.not_found
group.create_failed
group.update_failed
group.delete_failed
model.price_update_failed
model.price_delete_failed
```

## 任务清单

- [x] 迁移 Channel handler。
- [x] 迁移 Group handler。
- [x] 迁移 Model handler。
- [x] 补 locale。
- [x] 检查前端 toast 是否直接使用 `error.message`。

---

# 阶段 7：Relay 运行时错误

## 目标

迁移面向 API 使用方或日志展示的重要运行时错误。

## 建议错误码

```text
relay.model_not_supported
relay.model_not_found
relay.no_available_channel
relay.channel_disabled
relay.no_available_key
relay.upstream_failed
relay.timeout
relay.circuit_breaker_tripped
```

## 注意

Relay 是对外兼容 OpenAI/Anthropic 等 API 的核心路径，错误结构可能需要兼容外部协议，不应贸然统一为后台管理 API 的响应格式。

## 任务清单

- [x] 先梳理哪些 Relay 错误返回给管理后台，哪些返回给下游 API 客户端。
- [x] 管理后台可用 `error_code`。
- [x] 下游 API 兼容格式中可考虑增加内部错误码到日志，而非直接改变响应结构。
- [x] 先迁移日志/attempt 中的结构化错误，再考虑 API 响应。

---

# API 错误与同步结果消息边界

本计划优先迁移后台管理 API 的错误响应，即 HTTP 非 2xx 时返回的：

```json
{
  "code": 400,
  "error_code": "...",
  "message": "..."
}
```

以下字段属于同步结果/运行状态消息，不在阶段 0.5 和阶段 1 的 API error response 迁移范围内：

- `SiteSyncResult.Message`
- `SiteSyncGroupResult.Message`
- `SiteAccount.LastSyncMessage`
- `SiteCheckinResult.Message`

这些字段目前可能仍由后端返回中文自然语言。后续如果需要完整国际化，应单独设计 result code，例如：

```json
{
  "status": "partial",
  "message_code": "site.sync_result.partial",
  "message": "部分分组同步完成：更新 1 个分组，保留 1 个分组的历史模型",
  "params": {
    "synced": 1,
    "retained": 1
  }
}
```

---

# 阶段 8：前端旧字符串匹配清理

## 目标

在后端错误码覆盖足够后，清理前端旧的 message 正则/字符串映射。

## 主要涉及文件

- `web/src/components/modules/site/site-message.ts`
- 各模块中自行解析 `error.message` 的逻辑

## 任务清单

- [x] 标记所有仍依赖字符串匹配的地方。
- [x] 确认对应后端已返回 `error_code`。
- [x] 删除已不需要的 exact message map（保留并标记为 legacy fallback，以兼容旧同步结果/历史 last_sync_message）。
- [x] 删除已不需要的 regex match（保留并标记为 legacy fallback，以兼容旧同步结果/历史 last_sync_message）。
- [x] 保留一个通用 fallback：没有 `error_code` 时原样展示 `message`。

## 验收标准

- [x] 前端不再依赖新增的后端英文 message。
- [x] 旧未迁移接口仍能 fallback 展示。

---

# 每步执行模板

每迁移一个模块，按以下步骤执行：

1. 梳理当前错误字符串：

```bash
rg -n "fmt\.Errorf|errors\.New|resp\.Error" internal/<module> -S
```

2. 定义错误码常量。
3. 将固定业务错误改为 `apperror.New` / `Newf`。
4. 将包装错误改为 `apperror.Wrap`。
5. Handler 改为 `resp.ErrorWithAppError` 或 `resp.ErrorWithCode`。
6. 补 `web/public/locale/{zh_hans,zh_hant,en}.json`。
7. 补测试。
8. 执行验证：

```bash
go test ./...
cd web && pnpm lint
```

9. 单独提交：

```bash
git add <files>
git commit -m "feat: :globe_with_meridians: add coded errors for <module>"
```

---

# 风险与注意事项

## 不要一次性替换所有 `err.Error()`

很多底层错误包含重要上下文，直接替换可能降低可诊断性。建议稳定业务错误码 + 保留详细默认 message。

## 不要破坏外部 API 兼容

后台管理 API 可以统一 `error_code`，但 Relay 对 OpenAI/Anthropic 兼容响应需要单独设计。

## 错误码一旦发布应避免改名

错误码会被前端、脚本、用户自动化依赖。若需要废弃，应保留旧码一段时间。

## locale 缺失必须 fallback

前端当前已按以下顺序 fallback：

1. 当前语言
2. 简体中文
3. 英文
4. 后端默认 message

---

# 下一步建议

优先执行阶段 1：站点同步核心错误。

原因：

1. 站点同步错误最常直接展示给用户。
2. 当前已有 `site-message.ts` 的历史字符串映射，迁移收益明显。
3. Sub2API 同步错误已经接入，继续扩展同一模块成本最低。
