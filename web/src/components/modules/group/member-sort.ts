export type SortableGroupMember = {
    is_reserve?: boolean;
    balance?: number | null;
    multiplier?: number | null;
    group_multiplier?: number | null;
};

export function sortGroupMembers<T extends SortableGroupMember>(members: readonly T[]): T[] {
    return [...members].sort((a, b) => {
        const tierA = a.is_reserve ? 1 : 0;
        const tierB = b.is_reserve ? 1 : 0;
        if (tierA !== tierB) return tierA - tierB;

        const balanceA = a.balance ?? 0;
        const balanceB = b.balance ?? 0;
        const multiplierA = a.group_multiplier ?? a.multiplier ?? Number.POSITIVE_INFINITY;
        const multiplierB = b.group_multiplier ?? b.multiplier ?? Number.POSITIVE_INFINITY;
        if (tierA === 0) {
            if (balanceA !== balanceB) return balanceB - balanceA;
        } else {
            if (multiplierA !== multiplierB) return multiplierA - multiplierB;
            if (balanceA !== balanceB) return balanceB - balanceA;
        }
        return 0;
    });
}
