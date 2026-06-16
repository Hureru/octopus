'use client';

import { useState } from 'react';
import { ArrowDownWideNarrow, ArrowDownZA, ArrowUpAZ, ArrowUpNarrowWide, Clock3, LayoutGrid, List, Network, Plus, RefreshCw, Search, SlidersHorizontal, WandSparkles, X } from 'lucide-react';
import { motion, AnimatePresence } from 'motion/react';
import {
    MorphingDialog,
    MorphingDialogTrigger,
    MorphingDialogContainer,
    MorphingDialogContent,
} from '@/components/ui/morphing-dialog';
import { Popover, PopoverContent, PopoverTrigger } from '@/components/ui/popover';
import { buttonVariants } from '@/components/ui/button';
import { cn } from '@/lib/utils';
import { useNavStore, type NavItem } from '@/components/modules/navbar';
import { CreateDialogContent as ChannelCreateContent } from '@/components/modules/channel/Create';
import { CreateDialogContent as GroupCreateContent } from '@/components/modules/group/Create';
import { GroupAutoGroupDialogContent } from '@/components/modules/group/AutoGroupDialog';
import { CreateDialogContent as ModelCreateContent } from '@/components/modules/model/Create';
import { useSiteUIStore } from '@/components/modules/site/ui-store';
import { useLogUIStore } from '@/components/modules/log/ui-store';
import { LogFilterPopover } from '@/components/modules/log/FilterPopover';
import { useProxyPoolDialogStore } from '@/components/modules/proxy-pool/dialog-store';
import { useTranslations } from 'next-intl';
import { useSearchStore } from './search-store';
import {
    useToolbarViewOptionsStore,
    TOOLBAR_PAGES,
    type ToolbarPage,
    type ToolbarSortField,
    type ToolbarSortOrder,
} from './view-options-store';
type CombinedSortOption = {
    value: `${ToolbarSortField}-${ToolbarSortOrder}`;
    field: ToolbarSortField;
    order: ToolbarSortOrder;
    labelKey: string;
};
const COMBINED_SORT_OPTIONS: readonly CombinedSortOption[] = [
    { value: 'name-asc', field: 'name', order: 'asc', labelKey: 'popover.nameAsc' },
    { value: 'name-desc', field: 'name', order: 'desc', labelKey: 'popover.nameDesc' },
    { value: 'created-asc', field: 'created', order: 'asc', labelKey: 'popover.createdAsc' },
    { value: 'created-desc', field: 'created', order: 'desc', labelKey: 'popover.createdDesc' },
] as const;

const SITE_SORT_OPTIONS: readonly CombinedSortOption[] = [
    { value: 'name-asc', field: 'name', order: 'asc', labelKey: 'popover.nameAsc' },
    { value: 'name-desc', field: 'name', order: 'desc', labelKey: 'popover.nameDesc' },
    { value: 'balance-desc', field: 'balance', order: 'desc', labelKey: 'popover.balanceDesc' },
    { value: 'balance-asc', field: 'balance', order: 'asc', labelKey: 'popover.balanceAsc' },
] as const;

function isToolbarPage(item: NavItem): item is ToolbarPage {
    return (TOOLBAR_PAGES as readonly NavItem[]).includes(item);
}

function CreateDialogContent({ activeItem }: { activeItem: ToolbarPage }) {
    switch (activeItem) {
        case 'site':
            return null;
        case 'channel':
            return <ChannelCreateContent />;
        case 'group':
            return <GroupCreateContent />;
        case 'model':
            return <ModelCreateContent />;
        case 'log':
            return null;
    }
}

export function Toolbar() {
    const t = useTranslations('toolbar');
    const tProxyPool = useTranslations('proxyPool');
    const { activeItem } = useNavStore();
    const toolbarItem = isToolbarPage(activeItem) ? activeItem : null;
    const searchTerm = useSearchStore((s) => (toolbarItem ? s.searchTerms[toolbarItem] || '' : ''));
    const setSearchTerm = useSearchStore((s) => s.setSearchTerm);
    const layout = useToolbarViewOptionsStore((s) => (toolbarItem ? s.getLayout(toolbarItem) : 'grid'));
    const sortField = useToolbarViewOptionsStore((s) =>
        toolbarItem === 'site' || toolbarItem === 'channel' || toolbarItem === 'group' ? s.getSortField(toolbarItem) : 'name'
    );
    const sortOrder = useToolbarViewOptionsStore((s) => (toolbarItem ? s.getSortOrder(toolbarItem) : 'asc'));
    const setLayout = useToolbarViewOptionsStore((s) => s.setLayout);
    const setSortConfig = useToolbarViewOptionsStore((s) => s.setSortConfig);
    const setSortOrder = useToolbarViewOptionsStore((s) => s.setSortOrder);
    const requestOpenCreateSite = useSiteUIStore((s) => s.requestOpenCreateDialog);
    const requestOpenImportDialog = useSiteUIStore((s) => s.requestOpenImportDialog);
    const requestOpenArchivedDialog = useSiteUIStore((s) => s.requestOpenArchivedDialog);
    const requestSyncAll = useSiteUIStore((s) => s.requestSyncAll);
    const requestCheckinAll = useSiteUIStore((s) => s.requestCheckinAll);
    const requestLogRefresh = useLogUIStore((s) => s.requestRefresh);
    const isLogRefreshing = useLogUIStore((s) => s.isRefreshing);
    const openProxyPool = useProxyPoolDialogStore((s) => s.open);
    const [expandedSearchItem, setExpandedSearchItem] = useState<ToolbarPage | null>(null);
    const searchExpanded = expandedSearchItem === toolbarItem;

    if (!toolbarItem) return null;
    const isLogToolbar = toolbarItem === 'log';
    const showLayoutOptions = toolbarItem === 'channel' || toolbarItem === 'model';
    const showSiteSortOptions = toolbarItem === 'site';
    const showCombinedSortOptions = toolbarItem === 'channel' || toolbarItem === 'group';
    const showSortOptions = !isLogToolbar;


    return (
        <AnimatePresence mode="wait">
            <motion.div
                key="toolbar"
                initial={{ opacity: 0, scale: 0.9 }}
                animate={{ opacity: 1, scale: 1 }}
                exit={{ opacity: 0, scale: 0.9 }}
                transition={{ duration: 0.2 }}
                className="flex items-center gap-2"
            >
                {/* 搜索按钮/展开框 */}
                <div className="relative h-9 w-9">
                    {!searchExpanded ? (
                        <motion.button
                            layoutId="search-box"
                            onClick={() => setExpandedSearchItem(toolbarItem)}
                            className={buttonVariants({ variant: "ghost", size: "icon", className: "absolute inset-0 rounded-xl transition-none hover:bg-transparent text-muted-foreground hover:text-foreground" })}
                        >
                            <motion.span layout="position"><Search className="size-4 transition-colors duration-300" /></motion.span>
                        </motion.button>
                    ) : (
                        <motion.div
                            layoutId="search-box"
                            className="absolute right-0 top-0 flex items-center gap-2 h-9 px-3 rounded-xl border"
                            transition={{ type: 'spring', stiffness: 400, damping: 30 }}
                        >
                            <motion.span layout="position"><Search className="size-4 text-muted-foreground shrink-0" /></motion.span>
                            <input
                                type="text"
                                value={searchTerm}
                                onChange={(e) => setSearchTerm(toolbarItem, e.target.value)}
                                autoFocus
                                className="w-20 bg-transparent text-sm outline-none placeholder:text-muted-foreground"
                            />
                            <button
                                onClick={() => {
                                    setSearchTerm(toolbarItem, '');
                                    setExpandedSearchItem(null);
                                }}
                                className="p-0.5 rounded shrink-0 text-muted-foreground hover:text-foreground transition-colors"
                            >
                                <X className="size-3.5" />
                            </button>
                        </motion.div>
                    )}
                </div>

                {/* 站点页面的代理池按钮 */}
                {toolbarItem === 'site' && (
                    <motion.button
                        type="button"
                        aria-label={tProxyPool('name')}
                        title={tProxyPool('name')}
                        onClick={() => openProxyPool()}
                        initial={{ opacity: 0, scale: 0.9 }}
                        animate={{ opacity: 1, scale: 1 }}
                        exit={{ opacity: 0, scale: 0.9 }}
                        transition={{ duration: 0.2 }}
                        className={buttonVariants({ variant: "ghost", size: "icon", className: "rounded-xl transition-none hover:bg-transparent text-muted-foreground hover:text-foreground" })}
                    >
                        <Network className="size-4 transition-colors duration-300" />
                    </motion.button>
                )}

                {!isLogToolbar && <Popover>
                    <PopoverTrigger asChild>
                        <button
                            type="button"
                            aria-label={t('popover.ariaLabel')}
                            className={buttonVariants({
                                variant: 'ghost',
                                size: 'icon',
                                className: 'rounded-xl transition-none hover:bg-transparent text-muted-foreground hover:text-foreground',
                            })}
                        >
                            <SlidersHorizontal className="size-4 transition-colors duration-300" />
                        </button>
                    </PopoverTrigger>
                    <PopoverContent
                        align="center"
                        side="bottom"
                        sideOffset={8}
                        className="w-64 rounded-2xl border border-border/60 bg-card p-3 shadow-xl"
                    >
                        <div className="grid gap-3">
                            {showLayoutOptions && (
                                <div className="grid gap-2">
                                    <p className="text-xs font-medium text-muted-foreground">{t('popover.layout')}</p>
                                    <div className="grid grid-cols-2 gap-2">
                                        <button
                                            type="button"
                                            onClick={() => setLayout(toolbarItem, 'grid')}
                                            className={cn(
                                                'h-8 rounded-lg border text-xs font-medium inline-flex items-center justify-center gap-1.5 transition-colors',
                                                layout === 'grid'
                                                    ? 'border-primary/30 bg-primary text-primary-foreground'
                                                    : 'border-border bg-muted/20 text-foreground hover:bg-muted/30'
                                            )}
                                        >
                                            <LayoutGrid className="size-3.5" />
                                            {t('popover.grid')}
                                        </button>
                                        <button
                                            type="button"
                                            onClick={() => setLayout(toolbarItem, 'list')}
                                            className={cn(
                                                'h-8 rounded-lg border text-xs font-medium inline-flex items-center justify-center gap-1.5 transition-colors',
                                                layout === 'list'
                                                    ? 'border-primary/30 bg-primary text-primary-foreground'
                                                    : 'border-border bg-muted/20 text-foreground hover:bg-muted/30'
                                            )}
                                        >
                                            <List className="size-3.5" />
                                            {t('popover.list')}
                                        </button>
                                    </div>
                                </div>
                            )}

                            {showSortOptions && (
                                <div className="grid gap-2">
                                    <p className="text-xs font-medium text-muted-foreground">{t('popover.sort')}</p>
                                    {showSiteSortOptions ? (
                                        <div className="grid grid-cols-2 gap-2">
                                            {SITE_SORT_OPTIONS.map((option) => (
                                                <button
                                                    key={option.value}
                                                    type="button"
                                                    onClick={() => {
                                                        const active = sortField === option.field && sortOrder === option.order;
                                                        setSortConfig('site', active ? 'default' : option.field, active ? 'asc' : option.order);
                                                    }}
                                                    className={cn(
                                                        'h-8 rounded-lg border text-xs font-medium inline-flex items-center justify-center gap-1.5 transition-colors',
                                                        sortField === option.field && sortOrder === option.order
                                                            ? 'border-primary/30 bg-primary text-primary-foreground'
                                                            : 'border-border bg-muted/20 text-foreground hover:bg-muted/30'
                                                    )}
                                                >
                                                    {option.field === 'balance' ? (
                                                        option.order === 'desc' ? <ArrowDownWideNarrow className="size-3.5" /> : <ArrowUpNarrowWide className="size-3.5" />
                                                    ) : option.order === 'desc' ? (
                                                        <ArrowDownZA className="size-3.5" />
                                                    ) : (
                                                        <ArrowUpAZ className="size-3.5" />
                                                    )}
                                                    {t(option.labelKey)}
                                                </button>
                                            ))}
                                        </div>
                                    ) : showCombinedSortOptions ? (
                                        <div className="grid grid-cols-2 gap-2">
                                            {COMBINED_SORT_OPTIONS.map((option) => (
                                                <button
                                                    key={option.value}
                                                    type="button"
                                                    onClick={() => {
                                                        if (toolbarItem === 'channel' || toolbarItem === 'group') {
                                                            setSortConfig(toolbarItem, option.field, option.order);
                                                        }
                                                    }}
                                                    className={cn(
                                                        'h-8 rounded-lg border text-xs font-medium inline-flex items-center justify-center gap-1.5 transition-colors',
                                                        sortField === option.field && sortOrder === option.order
                                                            ? 'border-primary/30 bg-primary text-primary-foreground'
                                                            : 'border-border bg-muted/20 text-foreground hover:bg-muted/30'
                                                    )}
                                                >
                                                    {option.field === 'name' ? (
                                                        option.order === 'desc' ? <ArrowDownZA className="size-3.5" /> : <ArrowUpAZ className="size-3.5" />
                                                    ) : (
                                                        <Clock3 className="size-3.5" />
                                                    )}
                                                    {t(option.labelKey)}
                                                </button>
                                            ))}
                                        </div>
                                    ) : (
                                        <div className="grid grid-cols-2 gap-2">
                                            <button
                                                type="button"
                                                onClick={() => setSortOrder(toolbarItem, 'asc')}
                                                className={cn(
                                                    'h-8 rounded-lg border text-xs font-medium inline-flex items-center justify-center gap-1.5 transition-colors',
                                                    sortOrder === 'asc'
                                                        ? 'border-primary/30 bg-primary text-primary-foreground'
                                                        : 'border-border bg-muted/20 text-foreground hover:bg-muted/30'
                                                )}
                                            >
                                                <ArrowUpAZ className="size-3.5" />
                                                {t('popover.nameAsc')}
                                            </button>
                                            <button
                                                type="button"
                                                onClick={() => setSortOrder(toolbarItem, 'desc')}
                                                className={cn(
                                                    'h-8 rounded-lg border text-xs font-medium inline-flex items-center justify-center gap-1.5 transition-colors',
                                                    sortOrder === 'desc'
                                                        ? 'border-primary/30 bg-primary text-primary-foreground'
                                                        : 'border-border bg-muted/20 text-foreground hover:bg-muted/30'
                                                )}
                                            >
                                                <ArrowDownZA className="size-3.5" />
                                                {t('popover.nameDesc')}
                                            </button>
                                        </div>
                                    )}
                                </div>
                            )}

                            {toolbarItem === 'site' && (
                                <div className="grid gap-2">
                                    <p className="text-xs font-medium text-muted-foreground">全局操作</p>
                                    <div className="grid gap-2">
                                        <button
                                            type="button"
                                            onClick={requestOpenImportDialog}
                                            className="h-8 rounded-lg border px-2 text-xs font-medium text-left transition-colors border-border bg-muted/20 text-foreground hover:bg-muted/30"
                                        >
                                            导入站点数据
                                        </button>
                                        <button
                                            type="button"
                                            onClick={requestSyncAll}
                                            className="h-8 rounded-lg border px-2 text-xs font-medium text-left transition-colors border-border bg-muted/20 text-foreground hover:bg-muted/30"
                                        >
                                            全量同步
                                        </button>
                                        <button
                                            type="button"
                                            onClick={requestCheckinAll}
                                            className="h-8 rounded-lg border px-2 text-xs font-medium text-left transition-colors border-border bg-muted/20 text-foreground hover:bg-muted/30"
                                        >
                                            全量签到
                                        </button>
                                        <button
                                            type="button"
                                            onClick={requestOpenArchivedDialog}
                                            className="h-8 rounded-lg border px-2 text-xs font-medium text-left transition-colors border-border bg-muted/20 text-foreground hover:bg-muted/30"
                                        >
                                            归档站点
                                        </button>
                                    </div>
                                </div>
                            )}
                        </div>
                    </PopoverContent>
                </Popover>}

                {isLogToolbar && (
                    <>
                        <LogFilterPopover />

                        <button
                            type="button"
                            aria-label={t('popover.logRefresh.refresh')}
                            onClick={requestLogRefresh}
                            disabled={isLogRefreshing}
                            className={buttonVariants({
                                variant: 'ghost',
                                size: 'icon',
                                className: 'rounded-xl transition-none hover:bg-transparent text-muted-foreground hover:text-foreground disabled:opacity-100',
                            })}
                        >
                            <RefreshCw className={cn('size-4 transition-colors duration-300', isLogRefreshing && 'animate-spin')} />
                        </button>
                    </>
                )}

                {toolbarItem === 'group' && (
                    <MorphingDialog>
                        <MorphingDialogTrigger className={buttonVariants({ variant: "ghost", size: "icon", className: "rounded-xl transition-none hover:bg-transparent text-muted-foreground hover:text-foreground" })}>
                            <WandSparkles className="size-4 transition-colors duration-300" />
                        </MorphingDialogTrigger>

                        <MorphingDialogContainer>
                            <MorphingDialogContent className="w-fit max-w-full bg-card text-card-foreground px-6 py-4 rounded-3xl custom-shadow max-h-[calc(100vh-2rem)] flex flex-col overflow-hidden">
                                <GroupAutoGroupDialogContent />
                            </MorphingDialogContent>
                        </MorphingDialogContainer>
                    </MorphingDialog>
                )}

                {toolbarItem === 'site' ? (
                    <button
                        type="button"
                        onClick={requestOpenCreateSite}
                        className={buttonVariants({
                            variant: "ghost",
                            size: "icon",
                            className: "rounded-xl transition-none hover:bg-transparent text-muted-foreground hover:text-foreground",
                        })}
                    >
                        <Plus className="size-4 transition-colors duration-300" />
                    </button>
                ) : !isLogToolbar ? (
                    <MorphingDialog>
                        <MorphingDialogTrigger className={buttonVariants({ variant: "ghost", size: "icon", className: "rounded-xl transition-none hover:bg-transparent text-muted-foreground hover:text-foreground" })}>
                            <Plus className="size-4 transition-colors duration-300" />
                        </MorphingDialogTrigger>

                        <MorphingDialogContainer>
                            <MorphingDialogContent className="w-fit max-w-full bg-card text-card-foreground px-6 py-4 rounded-3xl custom-shadow max-h-[calc(100vh-2rem)] flex flex-col overflow-hidden">
                                <CreateDialogContent activeItem={toolbarItem} />
                            </MorphingDialogContent>
                        </MorphingDialogContainer>
                    </MorphingDialog>
                ) : null}
            </motion.div>
        </AnimatePresence>
    );
}

export { useSearchStore } from './search-store';
export { useToolbarViewOptionsStore } from './view-options-store';
