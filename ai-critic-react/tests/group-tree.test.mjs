import assert from 'node:assert/strict';
import { test } from 'node:test';
import {
    allCollapsed,
    branchPids,
    buildGroupForest,
    cardsFromVisible,
    defaultCollapsed,
    filterForest,
    flattenVisible,
    groupMembers,
    matchesNode,
    subtreePids,
} from '../src/v2/mcc/home/groupTree.ts';

function proc(pid, name, ppid = 1, extra = {}) {
    return {
        pid,
        ppid,
        name,
        cmd: `/usr/bin/${name.toLowerCase()} --pid=${pid}`,
        user: 'xhd2015',
        state: 'S',
        cpu_percent: 1,
        rss_bytes: 1024 * 1024,
        elapsed_sec: 3600,
        ...extra,
    };
}

test('groupMembers keeps only exact name matches', () => {
    const list = [proc(1, 'Google Chrome'), proc(2, 'Google Chrome Helper'), proc(3, 'Google Chrome')];
    assert.deepEqual(groupMembers(list, 'Google Chrome').map(p => p.pid), [1, 3]);
});

test('a linear chain yields one root and depths 0/1/2', () => {
    const list = [proc(1, 'app', 1), proc(2, 'app', 1), proc(3, 'app', 2)];
    list[1].ppid = 1;
    const forest = buildGroupForest(list, 'app');
    assert.equal(forest.roots.length, 1);
    assert.equal(forest.roots[0].proc.pid, 1);
    const rows = flattenVisible(forest.roots, new Set());
    assert.deepEqual(rows.map(r => [r.node.proc.pid, r.depth]), [
        [1, 0],
        [2, 1],
        [3, 2],
    ]);
});

test('two members parented outside the group make a forest, not a chain', () => {
    const list = [proc(10, 'chrome', 1), proc(20, 'chrome', 1), proc(30, 'chrome', 10)];
    const forest = buildGroupForest(list, 'chrome');
    assert.equal(forest.roots.length, 2);
    assert.equal(forest.rootCount, 2);
    assert.deepEqual(forest.roots.map(r => r.proc.pid).sort(), [10, 20]);
});

test('a parent outside the group is recorded on the root node', () => {
    const list = [proc(1, 'launchd', 0), proc(500, 'chrome', 1), proc(501, 'chrome', 500)];
    const forest = buildGroupForest(list, 'chrome');
    const root = forest.roots[0];
    assert.equal(root.proc.pid, 500);
    assert.equal(root.parentOutside?.name, 'launchd');
    assert.equal(root.depth, 0);
});

test('same name never implies parenting — only ppid links nodes', () => {
    // 900 and 901 share the name but 900 is parented to 1, which is outside the group.
    const list = [proc(400, 'chrome', 1), proc(900, 'chrome', 1), proc(901, 'chrome', 400)];
    const forest = buildGroupForest(list, 'chrome');
    const roots = forest.roots.map(r => r.proc.pid).sort();
    assert.deepEqual(roots, [400, 900]);
    const node400 = forest.roots.find(r => r.proc.pid === 400);
    assert.deepEqual(node400.children.map(c => c.proc.pid), [901]);
});

test('cross-group children stay out of the group tree', () => {
    const list = [proc(700, 'chrome', 1), proc(701, 'Renderer', 700), proc(702, 'chrome', 700)];
    const forest = buildGroupForest(list, 'chrome');
    assert.equal(forest.count, 2);
    const rows = flattenVisible(forest.roots, new Set());
    assert.deepEqual(rows.map(r => r.node.proc.pid), [700, 702]);
});

test('totals sum cpu, rss and take the oldest uptime', () => {
    const list = [
        proc(1, 'app', 1, { cpu_percent: 2.5, rss_bytes: 100, elapsed_sec: 60 }),
        proc(2, 'app', 1, { cpu_percent: 7.5, rss_bytes: 200, elapsed_sec: 172800 }),
    ];
    const forest = buildGroupForest(list, 'app');
    assert.equal(forest.cpu, 10);
    assert.equal(forest.rss, 300);
    assert.equal(forest.oldestSec, 172800);
    assert.equal(forest.count, 2);
});

test('collapsed pids hide their subtree', () => {
    const list = [proc(1, 'app', 1), proc(2, 'app', 1), proc(3, 'app', 2), proc(4, 'app', 3)];
    const forest = buildGroupForest(list, 'app');
    const rows = flattenVisible(forest.roots, new Set([2]));
    assert.deepEqual(rows.map(r => r.node.proc.pid), [1, 2]);
    assert.equal(rows[1].hasChildren, true);
    assert.equal(rows[1].expanded, false);
});

test('cardsFromVisible puts each depth-0 root in its own card', () => {
    const list = [proc(10, 'chrome', 1), proc(20, 'chrome', 1), proc(30, 'chrome', 10)];
    const forest = buildGroupForest(list, 'chrome');
    const cards = cardsFromVisible(flattenVisible(forest.roots, new Set()));
    assert.deepEqual(cards.map(c => c.map(r => r.node.proc.pid)), [
        [10, 30],
        [20],
    ]);
});

test('collapsing a leaf keeps it visible and marks it childless', () => {
    const list = [proc(1, 'app', 1), proc(2, 'app', 1)];
    const forest = buildGroupForest(list, 'app');
    const rows = flattenVisible(forest.roots, new Set([2]));
    assert.deepEqual(rows.map(r => r.node.proc.pid), [1, 2]);
    assert.equal(rows[1].hasChildren, false);
    assert.equal(rows[1].expanded, false);
});

test('subtreePids walks the whole branch', () => {
    const list = [proc(1, 'app', 1), proc(2, 'app', 1), proc(3, 'app', 2)];
    const forest = buildGroupForest(list, 'app');
    assert.deepEqual(subtreePids(forest.roots[0]).sort(), [1, 2, 3]);
});

test('filterForest keeps ancestors of matches and drops the rest', () => {
    const list = [
        proc(1, 'app', 1, { cmd: 'node server.js --pid=1' }),
        proc(2, 'app', 1, { cmd: 'node worker.js --pid=2' }),
        proc(3, 'app', 2, { cmd: 'node renderer --pid=3' }),
        proc(4, 'app', 1, { cmd: 'node idle --pid=4' }),
    ];
    const forest = buildGroupForest(list, 'app');
    const kept = filterForest(forest.roots, 'renderer');
    const rows = flattenVisible(kept, new Set());
    assert.deepEqual(rows.map(r => [r.node.proc.pid, r.depth]), [
        [1, 0],
        [2, 1],
        [3, 2],
    ]);
});

test('filterForest keeps the subtree of a directly matching node', () => {
    const list = [proc(1, 'app', 1, { cmd: 'node a' }), proc(2, 'app', 1, { cmd: 'node b' }), proc(3, 'app', 2, { cmd: 'node c' })];
    const forest = buildGroupForest(list, 'app');
    const kept = filterForest(forest.roots, 'node b');
    const rows = flattenVisible(kept, new Set());
    // The ancestor chain stays so the match keeps its place in the tree.
    assert.deepEqual(rows.map(r => r.node.proc.pid), [1, 2, 3]);
    assert.equal(rows[1].node.children.length, 1);
});

test('filterForest with a blank query is the identity', () => {
    const list = [proc(1, 'app', 1), proc(2, 'app', 1)];
    const forest = buildGroupForest(list, 'app');
    assert.equal(filterForest(forest.roots, '   '), forest.roots);
});

test('matchesNode searches name, cmd, user and pid', () => {
    const p = proc(4321, 'Google Chrome', 1, { cmd: '--type=renderer', user: 'xhd2015' });
    assert.equal(matchesNode(p, 'chrome'), true);
    assert.equal(matchesNode(p, 'renderer'), true);
    assert.equal(matchesNode(p, 'xhd2015'), true);
    assert.equal(matchesNode(p, '4321'), true);
    assert.equal(matchesNode(p, 'firefox'), false);
});

test('branchPids and allCollapsed only cover nodes with children', () => {
    const list = [proc(1, 'app', 1), proc(2, 'app', 1), proc(3, 'app', 2)];
    const forest = buildGroupForest(list, 'app');
    assert.deepEqual(branchPids(forest.roots).sort(), [1, 2]);
    assert.deepEqual([...allCollapsed(forest.roots)].sort(), [1, 2]);
});

test('defaultCollapsed opens the top levels and closes the rest', () => {
    const list = [proc(1, 'app', 1), proc(2, 'app', 1), proc(3, 'app', 2), proc(4, 'app', 3)];
    const forest = buildGroupForest(list, 'app');
    // limit 2: roots and their first level stay open, the level-2 branch closes.
    assert.deepEqual([...defaultCollapsed(forest.roots, 2)].sort(), [3]);
    // limit 0: only the roots themselves close; deeper levels are hidden anyway.
    assert.deepEqual([...defaultCollapsed(forest.roots, 0)].sort(), [1]);
    assert.deepEqual([...defaultCollapsed(forest.roots, 99)], []);
});

test('an empty group produces an empty forest', () => {
    const forest = buildGroupForest([proc(1, 'other', 1)], 'chrome');
    assert.equal(forest.count, 0);
    assert.deepEqual(forest.roots, []);
    assert.equal(forest.cpu, 0);
    assert.equal(forest.oldestSec, 0);
});

test('children are ordered by cpu descending, then pid', () => {
    const list = [
        proc(1, 'app', 0, { cpu_percent: 0.1 }),
        proc(2, 'app', 0, { cpu_percent: 9 }),
        proc(3, 'app', 0, { cpu_percent: 9 }),
        proc(4, 'app', 2, { cpu_percent: 0.5 }),
    ];
    const forest = buildGroupForest(list, 'app');
    assert.deepEqual(forest.roots.map(r => r.proc.pid), [2, 3, 1]);
    assert.deepEqual(forest.roots[0].children.map(c => c.proc.pid), [4]);
});
