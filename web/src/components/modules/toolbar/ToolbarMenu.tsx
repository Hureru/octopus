'use client';

import { type ReactNode } from 'react';
import { MoreHorizontal } from 'lucide-react';
import { Popover, PopoverContent, PopoverTrigger } from '@/components/ui/popover';
import { buttonVariants } from '@/components/ui/button';
import { cn } from '@/lib/utils';

export type ToolbarAction = {
    id: string;
    icon: ReactNode;
    label: string;
    onClick: () => void;
    disabled?: boolean;
    badge?: number;
    priority: 'always' | 'desktop' | 'large' | 'menu-only';
    // always:    始终可见（搜索）
    // desktop:   较常用，空间足够即平铺，否则折叠进"更多"（新增/刷新）
    // large:     次要，仅大屏平铺，否则折叠进"更多"（代理池/补全）
    // menu-only: 只在菜单（设置/页面操作）
};

interface ToolbarMenuProps {
    actions: ToolbarAction[];
}

export function ToolbarMenu({ actions }: ToolbarMenuProps) {
    const alwaysVisible = actions.filter((a) => a.priority === 'always');
    const desktopVisible = actions.filter((a) => a.priority === 'desktop');
    const largeVisible = actions.filter((a) => a.priority === 'large');
    const menuOnly = actions.filter((a) => a.priority === 'menu-only');

    // "更多"折叠只有在收纳 ≥2 项时才有意义；只剩 1 项时该项直接平铺，
    // 避免出现"更多里只有一个选项"（占位相同还多一次点击）。
    // 各断点会被折叠进菜单的项数：
    //   < md  ：large + desktop + menuOnly（desktop 在 md 以下折叠）
    //   md~xl ：仅 large ≥2 时的 large（单个 large 已平铺）+ menuOnly
    //   ≥ xl  ：menuOnly
    const collapsedBelowMd = largeVisible.length + desktopVisible.length + menuOnly.length;
    const collapsedMdToXl = (largeVisible.length > 1 ? largeVisible.length : 0) + menuOnly.length;
    const moreVisibleBelowMd = collapsedBelowMd >= 2;
    const moreVisibleMdToXl = collapsedMdToXl >= 2;

    // large 按钮：在"会被折叠且该区间确实显示更多"的断点收起，否则直接平铺
    const largeFlexClass =
        largeVisible.length > 1 && moreVisibleMdToXl
            ? 'hidden xl:flex'
            : largeVisible.length > 0 && moreVisibleBelowMd
            ? 'hidden md:flex'
            : 'flex';
    const largeMenuItemClass =
        largeVisible.length > 1 && moreVisibleMdToXl
            ? 'xl:hidden'
            : largeVisible.length > 0 && moreVisibleBelowMd
            ? 'md:hidden'
            : 'hidden';

    // desktop 按钮：仅在 <md 且该处确有 ≥2 项时折叠，否则始终平铺
    const desktopFlexClass =
        desktopVisible.length > 0 && moreVisibleBelowMd ? 'hidden md:flex' : 'flex';
    const desktopMenuItemClass =
        desktopVisible.length > 0 && moreVisibleBelowMd ? 'md:hidden' : 'hidden';

    // "更多"按钮：menuOnly 项只能在菜单内呈现，存在时始终显示；否则按区间项数决定
    const moreButtonVisibilityClass =
        menuOnly.length > 0
            ? ''
            : moreVisibleMdToXl
            ? 'xl:hidden'
            : moreVisibleBelowMd
            ? 'md:hidden'
            : 'hidden';

    // 是否渲染"更多"菜单（任一断点需要显示，或存在只能放菜单的项）
    const showMoreButton = menuOnly.length > 0 || moreVisibleBelowMd || moreVisibleMdToXl;

    return (
        <>
            {/* 始终可见的按钮 */}
            {alwaysVisible.map((action) => (
                <ActionButton key={action.id} action={action} />
            ))}

            {/* 大屏可见的按钮 - 单个 md 平铺、多个 xl 平铺 */}
            {largeVisible.length > 0 && (
                <div className={cn(largeFlexClass, 'items-center gap-2')}>
                    {largeVisible.map((action) => (
                        <ActionButton key={action.id} action={action} />
                    ))}
                </div>
            )}

            {/* 更多菜单 - 收纳折叠的按钮 */}
            {showMoreButton && (
                <Popover>
                    <PopoverTrigger asChild>
                        <button
                            type="button"
                            aria-label="更多操作"
                            className={cn(
                                buttonVariants({
                                    variant: 'ghost',
                                    size: 'icon',
                                    className:
                                        'rounded-xl transition-none hover:bg-transparent text-muted-foreground hover:text-foreground',
                                }),
                                moreButtonVisibilityClass
                            )}
                        >
                            <MoreHorizontal className="size-4 transition-colors duration-300" />
                        </button>
                    </PopoverTrigger>
                    <PopoverContent
                        align="end"
                        side="bottom"
                        sideOffset={8}
                        className="w-52 rounded-2xl border border-border/60 bg-card p-2 shadow-xl"
                    >
                        <div className="grid gap-1">
                            {/* 折叠时显示 large 按钮 */}
                            {largeVisible.length > 0 && (
                                <>
                                    <div className={largeMenuItemClass}>
                                        {largeVisible.map((action) => (
                                            <MenuActionItem key={action.id} action={action} />
                                        ))}
                                    </div>
                                    {(desktopVisible.length > 0 || menuOnly.length > 0) && (
                                        <div className="my-1 border-t border-border/60 md:hidden" />
                                    )}
                                </>
                            )}

                            {/* 折叠时显示 desktop 按钮 */}
                            {desktopVisible.length > 0 && (
                                <>
                                    <div className={desktopMenuItemClass}>
                                        {desktopVisible.map((action) => (
                                            <MenuActionItem key={action.id} action={action} />
                                        ))}
                                    </div>
                                    {menuOnly.length > 0 && (
                                        <div className="my-1 border-t border-border/60 md:hidden" />
                                    )}
                                </>
                            )}

                            {/* 始终在菜单的按钮（但我们现在没有使用 menu-only） */}
                            {menuOnly.map((action) => (
                                <MenuActionItem key={action.id} action={action} />
                            ))}
                        </div>
                    </PopoverContent>
                </Popover>
            )}

            {/* 中屏以上可见的按钮（如新增，置于最右）；单独成项时始终平铺 */}
            {desktopVisible.length > 0 && (
                <div className={cn(desktopFlexClass, 'items-center gap-2')}>
                    {desktopVisible.map((action) => (
                        <ActionButton key={action.id} action={action} />
                    ))}
                </div>
            )}
        </>
    );
}

// 按钮渲染（图标模式）
function ActionButton({ action }: { action: ToolbarAction }) {
    return (
        <button
            type="button"
            onClick={action.onClick}
            disabled={action.disabled}
            aria-label={action.label}
            title={action.label}
            className={cn(
                buttonVariants({
                    variant: 'ghost',
                    size: 'icon',
                    className:
                        'rounded-xl transition-none hover:bg-transparent text-muted-foreground hover:text-foreground relative',
                }),
                action.disabled && 'opacity-50 cursor-not-allowed',
            )}
        >
            {action.icon}
            {action.badge !== undefined && action.badge > 0 && (
                <span className="absolute -top-1 -right-1 flex h-4 min-w-4 items-center justify-center rounded-full bg-primary px-1 text-[10px] font-bold text-primary-foreground">
                    {action.badge > 99 ? '99+' : action.badge}
                </span>
            )}
        </button>
    );
}

// 菜单项渲染（文字+图标模式）
function MenuActionItem({ action }: { action: ToolbarAction }) {
    return (
        <button
            type="button"
            onClick={action.onClick}
            disabled={action.disabled}
            className={cn(
                'flex w-full items-center gap-3 rounded-xl px-3 py-2 text-sm text-left transition-colors hover:bg-muted/60',
                action.disabled && 'opacity-50 cursor-not-allowed',
            )}
        >
            <span className="text-muted-foreground [&>svg]:size-4">{action.icon}</span>
            <span className="flex-1">{action.label}</span>
            {action.badge !== undefined && action.badge > 0 && (
                <span className="ml-auto rounded-full bg-primary/10 px-2 py-0.5 text-xs font-medium text-primary">
                    {action.badge > 99 ? '99+' : action.badge}
                </span>
            )}
        </button>
    );
}
