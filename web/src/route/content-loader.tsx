'use client';

import { Suspense } from 'react';
import { useTranslations } from 'next-intl';
import { CONTENT_MAP } from './config';
import { ErrorBoundary } from './error-boundary';
import type { NavItem } from '@/components/modules/navbar/nav-store';

export function ContentLoader({ activeRoute }: { activeRoute: NavItem }) {
    const t = useTranslations('common');
    const Component = CONTENT_MAP[activeRoute];

    if (!Component) {
        return (
            <div className="flex items-center justify-center h-64">
                <p className="text-muted-foreground">{t('routeNotFound', { route: activeRoute })}</p>
            </div>
        );
    }

    return (
        <ErrorBoundary>
            <Suspense fallback={
                <div className="flex items-center justify-center h-64">
                    <div className="animate-pulse text-muted-foreground">{t('loading')}</div>
                </div>
            }>
                <Component />
            </Suspense>
        </ErrorBoundary>
    );
}
