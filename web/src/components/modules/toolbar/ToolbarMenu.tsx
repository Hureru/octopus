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
    // always: 始终可见（搜索）
    // desktop: md以上可见（新增）
    // large: xl以上可见（代理池/补全）
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

    // 根据响应式断点，决定哪些进入菜单
    const menuActions = [...largeVisible, ...desktopVisible, ...menuOnly];

    const hasMenuActions = menuActions.length > 0;

    return (
        <>
            {/* 始终可见的按钮 */}
            {alwaysVisible.map((action) => (
                <ActionButton key={action.id} action={action} />
            ))}

            {/* 中屏以上可见的按钮 - md:flex */}
            {desktopVisible.length > 0 && (
                <div className="hidden md:flex items-center gap-2">
                    {desktopVisible.map((action) => (
                        <ActionButton key={action.id} action={action} />
                    ))}
                </div>
            )}

            {/* 大屏可见的按钮 - xl:flex */}
            {largeVisible.length > 0 && (
                <div className="hidden xl:flex items-center gap-2">
                    {largeVisible.map((action) => (
                        <ActionButton key={action.id} action={action} />
                    ))}
                </div>
            )}

            {/* 更多菜单 - 收纳折叠的按钮 */}
            {hasMenuActions && (
                <Popover>
                    <PopoverTrigger asChild>
                        <button
                            type="button"
                            aria-label="更多操作"
                            className={buttonVariants({
                                variant: 'ghost',
                                size: 'icon',
                                className:
                                    'rounded-xl transition-none hover:bg-transparent text-muted-foreground hover:text-foreground',
                            })}
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
                            {/* xl以下显示 large 按钮 */}
                            {largeVisible.length > 0 && (
                                <>
                                    <div className="xl:hidden">
                                        {largeVisible.map((action) => (
                                            <MenuActionItem key={action.id} action={action} />
                                        ))}
                                    </div>
                                    {(desktopVisible.length > 0 || menuOnly.length > 0) && (
                                        <div className="my-1 border-t border-border/60 xl:hidden" />
                                    )}
                                </>
                            )}

                            {/* md以下显示 desktop 按钮 */}
                            {desktopVisible.length > 0 && (
                                <>
                                    <div className="md:hidden">
                                        {desktopVisible.map((action) => (
                                            <MenuActionItem key={action.id} action={action} />
                                        ))}
                                    </div>
                                    {menuOnly.length > 0 && (
                                        <div className="my-1 border-t border-border/60 md:hidden" />
                                    )}
                                </>
                            )}

                            {/* 始终在菜单的按钮 */}
                            {menuOnly.map((action) => (
                                <MenuActionItem key={action.id} action={action} />
                            ))}
                        </div>
                    </PopoverContent>
                </Popover>
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
