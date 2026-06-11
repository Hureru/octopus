'use client';

import { useCallback, useMemo, useState, type FormEvent } from 'react';
import { Plus, X, XIcon } from 'lucide-react';
import { Dialog, DialogContent } from '@/components/ui/dialog';
import { Button } from '@/components/ui/button';
import { Input } from '@/components/ui/input';
import {
    Select,
    SelectContent,
    SelectItem,
    SelectTrigger,
    SelectValue,
} from '@/components/ui/select';
import { toast } from '@/components/common/Toast';
import { type CustomHeader, useSiteBatchEdit } from '@/api/endpoints/site';
import { TagInput } from './TagInput';

type HeaderRowMode = 'set' | 'delete';

interface HeaderRow {
    mode: HeaderRowMode;
    header_key: string;
    header_value: string;
}

interface BatchEditDialogProps {
    open: boolean;
    onOpenChange: (open: boolean) => void;
    selectedSiteIds: number[];
    allTagNames: string[];
    selectedSiteTags: string[];
}

function createEmptyRow(): HeaderRow {
    return { mode: 'set', header_key: '', header_value: '' };
}

function getErrorMessage(error: unknown) {
    if (error instanceof Error) return error.message;
    if (typeof error === 'object' && error !== null && 'message' in error) {
        const message = (error as { message?: unknown }).message;
        if (typeof message === 'string') return message;
    }
    return '批量编辑失败';
}

/**
 * 多站点批量编辑弹窗（标签 + Header）。视觉风格与 SiteEditDialog 保持一致。
 * 只提交填写了的部分：标签先添加后移除（同名时移除优先）；Header「设置」行按 Key
 * 大小写不敏感 upsert，「删除」行按 Key 移除；各站点未涉及的内容保持不变
 * （合并逻辑由后端 /api/v1/site/batch/edit 执行）。
 */
export function BatchEditDialog({
    open,
    onOpenChange,
    selectedSiteIds,
    allTagNames,
    selectedSiteTags,
}: BatchEditDialogProps) {
    const batchEdit = useSiteBatchEdit();
    const [addTags, setAddTags] = useState<string[]>([]);
    const [removeTags, setRemoveTags] = useState<string[]>([]);
    const [rows, setRows] = useState<HeaderRow[]>(() => [createEmptyRow()]);

    const hasInput = useMemo(
        () =>
            addTags.length > 0 ||
            removeTags.length > 0 ||
            rows.some((row) => row.header_key.trim() !== ''),
        [addTags, removeTags, rows],
    );

    const handleOpenChange = useCallback(
        (next: boolean) => {
            if (!next) {
                setAddTags([]);
                setRemoveTags([]);
                setRows([createEmptyRow()]);
            }
            onOpenChange(next);
        },
        [onOpenChange],
    );

    const handleSubmit = useCallback(
        async (event: FormEvent<HTMLFormElement>) => {
            event.preventDefault();

            if (selectedSiteIds.length === 0) {
                toast.error('请先选择站点');
                return;
            }

            const upserts: CustomHeader[] = [];
            const deleteKeys: string[] = [];
            let invalid = false;
            for (const row of rows) {
                const key = row.header_key.trim();
                if (row.mode === 'delete') {
                    if (key) deleteKeys.push(key);
                    continue;
                }
                const value = row.header_value.trim();
                if (!key && !value) continue;
                if (!key || !value) {
                    invalid = true;
                    continue;
                }
                upserts.push({ header_key: key, header_value: value });
            }

            if (invalid) {
                toast.error('设置类 Header 的键和值都不能为空');
                return;
            }
            if (
                addTags.length === 0 &&
                removeTags.length === 0 &&
                upserts.length === 0 &&
                deleteKeys.length === 0
            ) {
                toast.error('请至少填写一项修改');
                return;
            }

            try {
                const result = await batchEdit.mutateAsync({
                    ids: selectedSiteIds,
                    add_tags: addTags,
                    remove_tags: removeTags,
                    upserts,
                    delete_keys: deleteKeys,
                });
                const successCount = result.success_ids.length;
                const failedCount = result.failed_items.length;
                toast.success(`操作完成：成功 ${successCount}，失败 ${failedCount}`);
                handleOpenChange(false);
            } catch (submitError) {
                toast.error(getErrorMessage(submitError));
            }
        },
        [rows, addTags, removeTags, selectedSiteIds, batchEdit, handleOpenChange],
    );

    return (
        <Dialog open={open} onOpenChange={handleOpenChange}>
            <DialogContent
                showCloseButton={false}
                className="w-screen max-w-full md:max-w-xl bg-card text-card-foreground px-6 py-4 rounded-3xl flex flex-col gap-0 border-0 sm:max-w-xl max-h-[min(calc(100vh-2rem),52rem)] overflow-hidden"
            >
                <header className="mb-4 flex items-start justify-between gap-4 shrink-0">
                    <div className="min-w-0 flex-1">
                        <h2 className="text-2xl font-bold text-card-foreground truncate">
                            批量编辑
                        </h2>
                        <p className="mt-1 text-sm text-muted-foreground">
                            将对 {selectedSiteIds.length} 个站点应用以下修改（未涉及的内容保持不变）
                        </p>
                    </div>
                    <button
                        type="button"
                        onClick={() => handleOpenChange(false)}
                        aria-label="关闭"
                        className="p-1 rounded-md text-muted-foreground hover:text-foreground hover:bg-muted transition-colors shrink-0"
                    >
                        <XIcon className="size-5" />
                    </button>
                </header>

                <form className="flex flex-1 min-h-0 flex-col" onSubmit={handleSubmit}>
                    <div className="flex-1 min-h-0 space-y-4 overflow-y-auto px-1">
                        <div className="space-y-1.5">
                            <label className="text-sm font-medium text-card-foreground">
                                添加标签
                            </label>
                            <TagInput
                                value={addTags}
                                onChange={setAddTags}
                                suggestions={allTagNames}
                            />
                        </div>
                        <div className="space-y-1.5">
                            <label className="text-sm font-medium text-card-foreground">
                                移除标签
                            </label>
                            <TagInput
                                value={removeTags}
                                onChange={setRemoveTags}
                                suggestions={selectedSiteTags}
                            />
                        </div>

                        <div className="border-t border-border/60" />

                        <div className="flex items-center justify-between">
                            <label className="text-sm font-medium text-card-foreground">
                                Header 列表 {rows.length > 0 ? `(${rows.length})` : ''}
                            </label>
                            <Button
                                type="button"
                                variant="ghost"
                                size="sm"
                                onClick={() =>
                                    setRows((current) => [...current, createEmptyRow()])
                                }
                                className="h-6 px-2 text-xs text-muted-foreground/70 hover:bg-transparent hover:text-muted-foreground"
                            >
                                <Plus className="mr-1 h-3 w-3" />
                                添加
                            </Button>
                        </div>
                        <div className="space-y-2">
                            {rows.map((row, index) => (
                                <div key={`batch-hdr-${index}`} className="flex items-center gap-2">
                                    <Select
                                        value={row.mode}
                                        onValueChange={(value) =>
                                            setRows((current) =>
                                                current.map((item, itemIndex) =>
                                                    itemIndex === index
                                                        ? { ...item, mode: value as HeaderRowMode }
                                                        : item,
                                                ),
                                            )
                                        }
                                    >
                                        <SelectTrigger className="w-[84px] shrink-0 rounded-xl">
                                            <SelectValue />
                                        </SelectTrigger>
                                        <SelectContent className="rounded-xl">
                                            <SelectItem className="rounded-xl" value="set">设置</SelectItem>
                                            <SelectItem className="rounded-xl" value="delete">删除</SelectItem>
                                        </SelectContent>
                                    </Select>
                                    <Input
                                        value={row.header_key}
                                        onChange={(event) =>
                                            setRows((current) =>
                                                current.map((item, itemIndex) =>
                                                    itemIndex === index
                                                        ? { ...item, header_key: event.target.value }
                                                        : item,
                                                ),
                                            )
                                        }
                                        placeholder="Header Key"
                                        className="flex-1 rounded-xl"
                                    />
                                    <Input
                                        value={row.mode === 'delete' ? '' : row.header_value}
                                        onChange={(event) =>
                                            setRows((current) =>
                                                current.map((item, itemIndex) =>
                                                    itemIndex === index
                                                        ? { ...item, header_value: event.target.value }
                                                        : item,
                                                ),
                                            )
                                        }
                                        placeholder={
                                            row.mode === 'delete' ? '删除此 Key（无需值）' : 'Header Value'
                                        }
                                        disabled={row.mode === 'delete'}
                                        className="flex-1 rounded-xl disabled:opacity-50"
                                    />
                                    <Button
                                        type="button"
                                        variant="ghost"
                                        size="sm"
                                        onClick={() =>
                                            setRows((current) =>
                                                current.filter((_, itemIndex) => itemIndex !== index),
                                            )
                                        }
                                        disabled={rows.length <= 1}
                                        className="h-8 w-8 rounded-xl p-0 text-muted-foreground hover:bg-transparent hover:text-destructive disabled:opacity-40"
                                        title="移除"
                                    >
                                        <X className="h-4 w-4" />
                                    </Button>
                                </div>
                            ))}
                        </div>
                        <p className="text-xs text-muted-foreground">
                            「设置」按 Key 新增或更新（大小写不敏感）；「删除」按 Key 移除。各站点其余 Header 保持不变。
                        </p>
                    </div>

                    <footer className="mt-5 flex shrink-0 flex-col gap-3 px-1 pt-2 sm:flex-row">
                        <Button
                            type="button"
                            variant="secondary"
                            className="h-12 w-full rounded-2xl sm:flex-1"
                            onClick={() => handleOpenChange(false)}
                        >
                            取消
                        </Button>
                        <Button
                            type="submit"
                            className="h-12 w-full rounded-2xl sm:flex-1"
                            disabled={
                                batchEdit.isPending ||
                                selectedSiteIds.length === 0 ||
                                !hasInput
                            }
                        >
                            {batchEdit.isPending ? '应用中...' : '应用到所选站点'}
                        </Button>
                    </footer>
                </form>
            </DialogContent>
        </Dialog>
    );
}
