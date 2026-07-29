'use client';

import { useMemo } from 'react';
import { useModelList } from '@/api/endpoints/model';
import { VirtualizedGrid } from '@/components/common/VirtualizedGrid';
import { useSearchStore, useToolbarViewOptionsStore } from '@/components/modules/toolbar';
import { ModelItem } from './Item';

export function LegacyPrices() {
    const { data: models } = useModelList();
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
