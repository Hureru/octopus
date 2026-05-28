# 站点投影状态治理实施进度

## 背景
站点模型同步失败后，历史 `site_models` 仍可能参与投影渠道与自动分组。上一版方案将“同步失败”直接映射为“暂停投影并删除 managed channel”，能阻止部分 stale 投影，但会误伤另一类常见场景：站点管理/用户凭据过期时，控制面无法同步，已投影的 API Key 仍可能正常可用。

## 最终目标
- 分离控制面同步失败、数据面 Key 失效、用户手动关闭投影三类语义。
- 控制面失败时保留现有投影，标记 stale/fetch failed，提示用户沿用上次成功结果。
- 只有权威确认不可服务时才进入系统暂停，例如缺少可用 Key、权威空模型列表、分组被权威移除或运行时确认全部 Key 不可用。
- 系统自动暂停以禁用 managed channel 为主，不删除 channel、binding、group item 等用户配置和历史关联。
- 用户手动 `projection_disabled` 仍与系统态分离，继续表达“用户不希望投影”。
- 同步恢复后自动清除系统暂停并复用原 managed channel。

## 设计决策
- `projection_disabled` 保留为用户态；系统态继续使用 `projection_suspended`、`projection_suspend_reason`、`projection_suspended_at`。
- `failed` / `unresolved` 表示本次无法确认模型，保留历史模型和历史投影，不设置系统暂停。
- `stale` 表示账号级控制面失败，仅更新分组同步状态，不动投影渠道。
- `missing_key` 与权威 `empty` 进入 hard suspend：保留历史关系，但禁用对应 managed channel。
- `removed` 表示权威移除分组，清理历史模型；移除类 stale binding 可删除。
- `ProjectAccount` 对 active/stale group 继续投影；对系统暂停 group 禁用已有 managed channel；对用户关闭或真正 stale/orphan binding 才删除。
- 账号级 `snapshot == nil` 的同步失败不再调用删除式暂停，而是标记非暂停 stale 状态。
- 手动同步即使返回业务失败，前端也需要在 settled 后刷新相关查询，避免后端状态已改变但 UI 不更新。

## 待办
- [x] 重新定义最终方案并更新进度文档
- [x] 扩展模型状态与前端类型
- [x] 调整账号级失败处理为 stale-kept
- [x] 调整分组同步状态持久化：failed/unresolved 不暂停，missing/empty 才暂停
- [x] 调整投影逻辑：系统暂停禁用 channel，不删除 binding/group item
- [x] 修正 AnyRouter 失败快照持久化路径
- [x] 调整前端展示：区分 stale warning 与 hard suspended
- [x] 补充/更新测试覆盖控制面失败、soft stale、hard suspend disable、恢复路径
- [x] 运行 gofmt、Go 测试与前端 lint

## 实施日志
- 2026-05-28：创建初版站点投影自动暂停方案。
- 2026-05-28：根据控制面凭据过期会误伤投影 Key 的风险，重写为 stale-kept + hard suspend disable 的完整方案。
- 2026-05-28：新增 `stale` 分组模型同步状态；账号级控制面失败改为 `MarkAccountProjectionStale`，不再删除/禁用投影渠道。
- 2026-05-28：`failed` / `unresolved` 改为保留历史投影；`missing_key` / 权威 `empty` 才设置系统暂停。
- 2026-05-28：系统暂停的 managed channel 改为禁用并保留 binding 与 group item；用户手动关闭投影仍按原语义清理。
- 2026-05-28：AnyRouter 全部分组失败时也返回 snapshot，保证失败状态可持久化。
- 2026-05-28：前端区分“沿用历史投影”和“系统暂停”，系统暂停时禁用用户投影按钮，并在同步 settled 后刷新查询。
- 2026-05-28：更新测试并通过 `go test ./internal/sitesync`、`go test ./...`、`cd web && pnpm lint`。
