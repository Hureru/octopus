# 交接说明：站点页改为单列列表 + 账号卡片快捷开关

> 面向接手的 AI / 工程师。描述站点页（`web/src/components/modules/site/`）最近的 UI 改动。
>
> 分支：`feat/model-group-provisioning`（与模型供给功能同一工作区，改的是不同主题）
> 状态：**已完成，lint/build 通过，尚未 commit**
> 日期：2026-08-02

---

## 1. 要解决的问题

站点页此前桌面端采用**双列瀑布流**（`md:grid md:grid-cols-2`，通过测量站点卡片高度做 masonry 平衡）。用户希望：

1. 改成**单列全宽列表形式**，从上到下纵向排列，一行一个站点卡片。
2. 账号卡片里提供**快捷开关**：`enabled`（启用/停用账号）与 `auto_sync`（自动同步），不用进弹窗即可切换；`proxy_mode` 是多值枚举，保持弹窗里的下拉选择，不做成开关。

---

## 2. 改动内容

只动了一个文件：`web/src/components/modules/site/index.tsx`。

### 2.1 布局：双列瀑布流 → 单列全宽

- 删除 `masonryColumns` useMemo（原来按估算高度把站点分左右两列）。
- 删除仅用于分列的 `estimateVisibleSiteCardHeight`、`siteCardHeights` state、`cardObserversRef`（ResizeObserver 高度测量）。
- `setSiteCardMeasureRef` 简化为纯 ref 注册（保留 `cardElementsRef`，供「跳转滚动定位」用），并同步精简了卸载清理 effect。
- 渲染处合并成单个 `<div className="space-y-4">`，`visibleSites.map` 依次输出每个站点卡片，移除原来的 `md:hidden`（移动端单列）与 `hidden md:grid md:grid-cols-2`（桌面双列）两套分支。
- 原有「移动端顶部工具栏重叠时的 pb 间距」不变。

### 2.2 账号卡片 → 新增「自动同步」开关

- 新增 `const updateSiteAccount = useUpdateSiteAccount()`（`web/src/api/endpoints/site.ts` 已有，走 `POST /api/v1/site/account/update`）。
- 新增 `handleToggleAutoSync(account)`：`updateSiteAccount.mutateAsync({ id, auto_sync: !account.auto_sync })`，成功/失败用现有 `getSiteErrorMessage` + toast 提示。
- 账号卡片操作区新增第二个 `Switch`（工具提示「开启/关闭自动同步」），与既有「启用/停用账号」开关并排，`disabled` 绑定 `updateSiteAccount.isPending`。
- `proxy_mode` 未动：仍以文本展示（`inherit/direct/system/pool`），完整代理配置走 `AccountEditDialog` 的 `ProxySelector`。

> 文案沿用文件内的硬编码中文（该页面未走 i18n），与既有按钮文案风格一致。

---

## 3. 验证

```bash
cd web
corepack pnpm lint     # 0 error 0 warning
corepack pnpm build    # 含 TypeScript 检查，通过
```

`updateSiteAccount` 成功后 `useUpdateSiteAccount` 会 invalidate `sites/list`、`site-channel` 等查询（`invalidateSiteQueries`），开关状态即时刷新。

---

## 4. 已知遗留 / 不在本次范围

1. **开关 pending 是全局的**：`updateSiteAccount.isPending` 一下只影响所有账号卡片的开关同时禁用（`useEnableSiteAccount` 的既有行为也是如此），未做单账号级 pending 追踪。数据量不大，可接受。
2. **站点卡片本身仍为「卡片 + 展开账号」结构**，只是列数从 2 变 1；没有改成表格式的横向列布局。用户在确认时选了「单列全宽」而非「表格风格」。
3. 若后续要支持「代理模式」也成为卡片内快捷操作，需要先确定枚举到开关的语义（如「是否独立代理」on=pool/system/direct、off=inherit），本轮不处理。

---

## 5. 工作区状态（接手前必读）

- 与 `HANDOFF-model-group-provisioning.md` 描述的是**同一工作区、同一分支**，改动未 commit。
- 站点页改动与本份文档文件的清单：

```
# 改
web/src/components/modules/site/index.tsx
```