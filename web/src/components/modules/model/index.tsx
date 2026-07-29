'use client';

import { useState } from 'react';
import { Boxes, DollarSign, ShieldCheck } from 'lucide-react';
import { useTranslations } from 'next-intl';
import { cn } from '@/lib/utils';
import { ModelCatalog } from './Catalog';
import { HeaderPolicies } from './HeaderPolicies';
import { LegacyPrices } from './LegacyPrices';

type ModelView = 'catalog' | 'headers' | 'global-prices';

const VIEWS = [
    { id: 'catalog', icon: Boxes },
    { id: 'headers', icon: ShieldCheck },
    { id: 'global-prices', icon: DollarSign },
] as const;

export function Model() {
    const t = useTranslations('model.workspace');
    const [view, setView] = useState<ModelView>('catalog');

    return (
        <div className="flex h-full min-h-0 flex-col gap-3">
            <div
                role="tablist"
                aria-label={t('ariaLabel')}
                className="flex shrink-0 gap-1 overflow-x-auto rounded-lg border bg-card p-1"
            >
                {VIEWS.map((item) => {
                    const active = view === item.id;
                    return (
                        <button
                            key={item.id}
                            type="button"
                            role="tab"
                            id={`model-tab-${item.id}`}
                            aria-controls={`model-panel-${item.id}`}
                            aria-selected={active}
                            tabIndex={active ? 0 : -1}
                            onKeyDown={(event) => {
                                const index = VIEWS.findIndex((candidate) => candidate.id === item.id);
                                let nextIndex = index;
                                if (event.key === 'ArrowRight' || event.key === 'ArrowDown') {
                                    nextIndex = (index + 1) % VIEWS.length;
                                } else if (event.key === 'ArrowLeft' || event.key === 'ArrowUp') {
                                    nextIndex = (index - 1 + VIEWS.length) % VIEWS.length;
                                } else if (event.key === 'Home') {
                                    nextIndex = 0;
                                } else if (event.key === 'End') {
                                    nextIndex = VIEWS.length - 1;
                                } else {
                                    return;
                                }
                                event.preventDefault();
                                const next = VIEWS[nextIndex].id;
                                setView(next);
                                requestAnimationFrame(() => {
                                    document.getElementById(`model-tab-${next}`)?.focus();
                                });
                            }}
                            onClick={() => setView(item.id)}
                            className={cn(
                                'inline-flex min-h-10 min-w-fit flex-1 items-center justify-center gap-2 rounded-md px-3 text-sm font-medium transition-colors',
                                active
                                    ? 'bg-primary text-primary-foreground'
                                    : 'text-muted-foreground hover:bg-muted hover:text-foreground',
                            )}
                        >
                            <item.icon className="size-4" />
                            {t(item.id)}
                        </button>
                    );
                })}
            </div>
            <div
                role="tabpanel"
                id={`model-panel-${view}`}
                aria-labelledby={`model-tab-${view}`}
                className="min-h-0 flex-1 overflow-hidden pb-24 md:pb-0"
            >
                {view === 'catalog' ? <ModelCatalog /> : null}
                {view === 'headers' ? <HeaderPolicies /> : null}
                {view === 'global-prices' ? <LegacyPrices /> : null}
            </div>
        </div>
    );
}
