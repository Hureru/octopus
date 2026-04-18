import { useCallback, useEffect, useMemo, useRef, useState } from 'react';

export interface UseMasonryColumnsOptions<T> {
    items: T[];
    getId: (item: T) => string | number;
    fallbackHeight: number;
    computeColumnCount: (width: number) => number;
}

export interface UseMasonryColumnsReturn<T> {
    setContainerNode: (node: HTMLElement | null) => void;
    measureItem: (id: string | number, node: HTMLElement | null) => void;
    columns: T[][];
    columnCount: number;
}

export function useMasonryColumns<T>({
    items,
    getId,
    fallbackHeight,
    computeColumnCount,
}: UseMasonryColumnsOptions<T>): UseMasonryColumnsReturn<T> {
    const [containerEl, setContainerEl] = useState<HTMLElement | null>(null);
    const [width, setWidth] = useState<number>(() =>
        typeof window === 'undefined' ? 1024 : window.innerWidth,
    );
    const [heights, setHeights] = useState<Record<string, number>>({});
    const observersRef = useRef<Map<string, ResizeObserver>>(new Map());
    const elementsRef = useRef<Map<string, HTMLElement>>(new Map());

    useEffect(() => {
        if (!containerEl) return;
        const update = () => {
            const next = containerEl.clientWidth;
            setWidth((prev) => (prev === next ? prev : next));
        };
        update();
        if (typeof ResizeObserver === 'undefined') return;
        const observer = new ResizeObserver(update);
        observer.observe(containerEl);
        return () => observer.disconnect();
    }, [containerEl]);

    useEffect(() => {
        const observers = observersRef.current;
        return () => {
            observers.forEach((o) => o.disconnect());
            observers.clear();
        };
    }, []);

    const measureItem = useCallback((id: string | number, node: HTMLElement | null) => {
        const key = String(id);
        const elements = elementsRef.current;
        const observers = observersRef.current;
        const current = elements.get(key);

        if (current === node) return;

        if (current) {
            observers.get(key)?.disconnect();
            observers.delete(key);
            elements.delete(key);
        }

        if (!node) {
            // Keep height in state; the item may just be re-parented during
            // masonry reshuffling — clearing it would trigger oscillation.
            return;
        }

        elements.set(key, node);
        const observer = new ResizeObserver((entries) => {
            const next = Math.round(
                entries[0]?.contentRect.height ?? node.getBoundingClientRect().height,
            );
            setHeights((curr) => (curr[key] === next ? curr : { ...curr, [key]: next }));
        });
        observer.observe(node);
        observers.set(key, observer);

        const initial = Math.round(node.getBoundingClientRect().height);
        setHeights((curr) => (curr[key] === initial ? curr : { ...curr, [key]: initial }));
    }, []);

    const columnCount = useMemo(
        () => Math.max(1, computeColumnCount(width)),
        [computeColumnCount, width],
    );

    const columns = useMemo(() => {
        const cols: T[][] = Array.from({ length: columnCount }, () => []);
        const colHeights = new Array(columnCount).fill(0) as number[];
        for (const item of items) {
            const key = String(getId(item));
            const h = heights[key] ?? fallbackHeight;
            let shortest = 0;
            for (let i = 1; i < colHeights.length; i++) {
                if (colHeights[i]! < colHeights[shortest]!) shortest = i;
            }
            cols[shortest]!.push(item);
            colHeights[shortest]! += h;
        }
        return cols;
    }, [items, columnCount, heights, getId, fallbackHeight]);

    return {
        setContainerNode: setContainerEl,
        measureItem,
        columns,
        columnCount,
    };
}
