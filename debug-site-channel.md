# 投影渠道滚动问题诊断

## 问题现象
投影渠道管理面板在滚动到约45条后无法继续向下滚动。

## 可能原因

### 1. 数据确实只有45条
打开浏览器开发者工具，在 Console 中运行：
```js
// 检查实际数据量
console.log('Total site channel cards:', document.querySelectorAll('[data-index]').length);
```

### 2. 虚拟滚动容器高度问题
检查容器高度：
```js
// 找到滚动容器
const container = document.querySelector('.overflow-y-auto.overscroll-contain');
console.log('Container height:', container?.clientHeight);
console.log('Scroll height:', container?.scrollHeight);
console.log('Scroll top:', container?.scrollTop);
```

### 3. 父容器高度限制
检查父容器：
```js
// 检查 Channel 组件的容器
const channelRoot = document.querySelector('.flex.h-full.min-h-0.flex-col');
console.log('Channel root height:', channelRoot?.clientHeight);
```

## 临时调试补丁

如果想临时查看所有数据（禁用虚拟滚动），可以修改 `VirtualizedGrid.tsx`：

在 188 行的 `<div className="relative w-full" style={{ height: ...` 后添加：
```tsx
style={{ height: `${rowVirtualizer.getTotalSize()}px`, minHeight: '100%' }}
```

## 建议的修复

如果确认是虚拟滚动的问题，需要：

1. 检查 `web/src/components/modules/channel/index.tsx` 第 157-158 行的容器结构
2. 确保 `.flex-1 min-h-0` 样式正确传递高度
3. 检查是否有 CSS `max-height` 限制

## 快速验证

在浏览器 Console 中运行：
```js
// 强制滚动到底部
const container = document.querySelector('.overflow-y-auto.overscroll-contain');
if (container) {
  container.scrollTop = container.scrollHeight;
  console.log('Scrolled to:', container.scrollTop);
}
```

如果可以滚到底，说明数据确实只有45条。
如果滚不到底，说明是虚拟滚动容器高度计算问题。
