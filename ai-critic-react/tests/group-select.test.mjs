import assert from 'node:assert/strict';
import { test } from 'node:test';
import { pruneSelected, selectableRootPids } from '../src/v2/mcc/home/groupSelect.ts';

const row = (pid, depth, extra = {}) => ({
    depth,
    hasChildren: false,
    expanded: false,
    node: {
        proc: { pid, protected: extra.protected },
        depth,
        children: [],
        isRoot: depth === 0,
    },
});

test('selectableRootPids keeps unprotected depth-0 rows only', () => {
    const rows = [
        row(10, 0),
        row(11, 1),
        row(20, 0, { protected: 'system' }),
        row(21, 0),
    ];
    assert.deepEqual(selectableRootPids(rows), [10, 21]);
});

test('pruneSelected drops pids that are no longer selectable', () => {
    const next = pruneSelected(new Set([10, 11, 99]), [10, 21]);
    assert.deepEqual([...next].sort((a, b) => a - b), [10]);
});
