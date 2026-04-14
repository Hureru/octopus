'use client';

import { Suspense } from 'react';
import { CONTENT_MAP } from './config';
import { ErrorBoundary } from './error-boundary';
import type { NavItem } from '@/components/modules/navbar/nav-store';

export function ContentLoader({ activeRoute }: { activeRoute: NavItem }) {
    const Component = CONTENT_MAP[activeRoute];

    if (!Component) {
        return (
            <div className="flex items-center justify-center h-64">
                <p className="text-muted-foreground">Route not found: {activeRoute}</p>
            </div>
        );
    }

    return (
        <ErrorBoundary>
            <Suspense fallback={
                <div className="flex items-center justify-center h-64">
                    <div className="animate-pulse text-muted-foreground">Loading...</div>
                </div>
            }>
                <Component />
            </Suspense>
        </ErrorBoundary>
    );
}
