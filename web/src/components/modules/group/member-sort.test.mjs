import assert from 'node:assert/strict';
import test from 'node:test';

import { sortGroupMembers } from './member-sort.ts';

test('sorts relay members by API Key group multiplier including zero', () => {
    const members = [
        { id: 'legacy', is_reserve: true, balance: 20, multiplier: 0.1, group_multiplier: null },
        { id: 'paid', is_reserve: true, balance: 30, multiplier: 0.01, group_multiplier: 0.2 },
        { id: 'free', is_reserve: true, balance: 10, multiplier: 1, group_multiplier: 0 },
    ];

    assert.deepEqual(
        sortGroupMembers(members).map((member) => member.id),
        ['free', 'legacy', 'paid'],
    );
});

test('keeps direct channels ahead of relay channels and sorts them by balance', () => {
    const members = [
        { id: 'relay', is_reserve: true, balance: 100, group_multiplier: 0 },
        { id: 'direct-low', is_reserve: false, balance: 10, group_multiplier: 5 },
        { id: 'direct-high', is_reserve: false, balance: 20, group_multiplier: 10 },
    ];

    assert.deepEqual(
        sortGroupMembers(members).map((member) => member.id),
        ['direct-high', 'direct-low', 'relay'],
    );
});
