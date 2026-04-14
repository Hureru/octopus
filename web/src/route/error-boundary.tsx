'use client';

import { Component, type ReactNode, type ErrorInfo } from 'react';

interface Props {
    children: ReactNode;
    fallback?: ReactNode;
}

interface State {
    hasError: boolean;
}

export class ErrorBoundary extends Component<Props, State> {
    constructor(props: Props) {
        super(props);
        this.state = { hasError: false };
    }

    static getDerivedStateFromError(): State {
        return { hasError: true };
    }

    componentDidCatch(error: Error, errorInfo: ErrorInfo) {
        console.error('Route component error:', error, errorInfo);
    }

    render() {
        if (this.state.hasError) {
            return this.props.fallback ?? (
                <div className="flex items-center justify-center h-64">
                    <p className="text-muted-foreground">
                        Something went wrong. Please refresh the page.
                    </p>
                </div>
            );
        }
        return this.props.children;
    }
}
