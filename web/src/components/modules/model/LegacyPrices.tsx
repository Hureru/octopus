'use client';

import { useMemo } from 'react';
import { useTranslations } from 'next-intl';
import { useModelList } from '@/api/endpoints/model';
import { Button } from '@/components/ui/button';
import { VirtualizedGrid } from '@/components/common/VirtualizedGrid';
import { useSearchStore, useToolbarViewOptionsStore } from '@/components/modules/toolbar';
import { ModelItem } from './Item';

export function LegacyPrices() {
    const t = useTranslations('model.catalog');
    const modelsQuery = useModelList();
    const { data: models } = modelsQuery;
    const pageKey = 'model' as const;
    const searchTerm = useSearchStore((state) => state.getSearchTerm(pageKey));
    const layout = useToolbarViewOptionsStore((state) => state.getLayout(pageKey));
    const sortOrder = useToolbarViewOptionsStore((state) => state.getSortOrder(pageKey));

    const visibleModels = useMemo(() => {
        const term = searchTerm.toLowerCase().trim();
        return [...(models ?? [])]
            .sort((left, right) =>
                sortOrder === 'asc'
                    ? left.name.localeCompare(right.name)
                    : right.name.localeCompare(left.name),
            )
            .filter((model) => !term || model.name.toLowerCase().includes(term));
    }, [models, searchTerm, sortOrder]);

    if (modelsQuery.error) {
        return (
            <div role="alert" className="grid h-full min-h-40 place-items-center p-4 text-center text-sm text-destructive">
                <div>
                    <p>{modelsQuery.error instanceof Error ? modelsQuery.error.message : String(modelsQuery.error)}</p>
                    <Button type="button" variant="outline" size="sm" className="mt-3" onClick={() => void modelsQuery.refetch()}>
                        {t('retry')}
                    </Button>
                </div>
            </div>
        );
    }

    return (
        <VirtualizedGrid
            items={visibleModels}
            layout={layout}
            columns={{ default: 1, md: 2, lg: 3 }}
            estimateItemHeight={112}
            getItemKey={(model) => `model-${model.name}`}
            renderItem={(model) => <ModelItem model={model} layout={layout} />}
        />
    );
}
