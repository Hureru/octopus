'use client';

import { useEffect } from 'react';
import { createPortal } from 'react-dom';
import { motion } from 'motion/react';

// Key 卡片编辑/统计/导出浮层的共用顶层容器。
// 浮层原先 absolute 定位在卡片内，超出设置页 overflow-y-auto 容器顶边的部分
// 会被裁切（视觉上被 header 挡住），portal 到 body 并 fixed 定位后不受裁切影响。
// data-slot 复用 MorphingDialog 的 PORTAL_IGNORED_SLOTS 机制：
// 浮层在放大视图中打开时，点击/Escape 不会连带关闭底层对话框。
// z-50 与 MorphingDialog/Popover/Select 一致，层级由 portal 挂载顺序（后开在上）决定。
export function OverlayPortal({ onClose, children }: { onClose: () => void; children: React.ReactNode }) {
    useEffect(() => {
        const handleKeyDown = (event: KeyboardEvent) => {
            // Radix Select/Popover 展开时 Escape 已被其消费（defaultPrevented），不连带关闭浮层
            if (event.key === 'Escape' && !event.defaultPrevented) onClose();
        };
        document.addEventListener('keydown', handleKeyDown);
        return () => document.removeEventListener('keydown', handleKeyDown);
    }, [onClose]);

    return createPortal(
        <>
            <motion.div
                data-slot="dialog-overlay"
                aria-hidden="true"
                className="fixed inset-0 z-50 bg-white/40 backdrop-blur-xs dark:bg-black/40"
                initial={{ opacity: 0 }}
                animate={{ opacity: 1 }}
                exit={{ opacity: 0 }}
                onClick={onClose}
            />
            {children}
        </>,
        document.body
    );
}
