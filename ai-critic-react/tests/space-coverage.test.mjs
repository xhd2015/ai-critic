import assert from 'node:assert/strict';
import { test } from 'node:test';
import {
    budgetClock,
    budgetWords,
    coverageText,
    coverageView,
    groupDetail,
    inlineGroups,
    inlineRest,
    partialNote,
    priorityFraction,
    rootStateLabel,
    rootStateMark,
    showsFullDiskAccessHint,
    skipKindClass,
    skipKindLabel,
} from '../src/v2/mcc/home/spaceCoverageTone.ts';

const group = (prefix, count, kind, reason, example) => ({ prefix, count, kind, reason, example });

/** The live report shape: 490 dirs missing, 8 folded groups, budget hit at 10 min. */
const live = {
    id: '20261114-203720',
    complete: false,
    truncated: true,
    no_access_dirs: 432,
    not_counted_total: 490,
    scanned_at: '2026-11-14T20:37:20+08:00',
    categories: [],
    scan_ms: 600_000,
    files: 9_786_739,
    scanned_bytes: 430_000_000_000,
    coverage: {
        priority_roots: ['~', '/Library/Developer', '/Library/Caches', '/Applications', '/private/var/folders', '/opt'],
        priority_scanned: 3,
        priority_complete: false,
        priority_states: [
            { root: '~', state: 'done' },
            { root: '/Library/Developer', state: 'partial' },
            { root: '/Library/Caches', state: 'partial' },
            { root: '/Applications', state: 'not_started' },
            { root: '/private/var/folders', state: 'not_started' },
            { root: '/opt', state: 'not_started' },
        ],
        budget_s: 600,
        elapsed_s: 600.2,
        truncated: true,
        not_counted_total: 490,
        skip_groups: [
            group('~/Library/Containers', 283, 'permission', 'operation not permitted'),
            group('~/Library/Containers/com.apple.Safari/Data', 96, 'permission', 'operation not permitted'),
            group('~/Library/Containers/com.apple.Photos', 38, 'timeout', 'operation timed out after 1s'),
            group('~/Library/Group Containers', 24, 'timeout', 'operation timed out after 1s'),
            group('~/Library/Application Support', 41, 'io', 'input/output error'),
            group('~/Library/CloudStorage', 6, 'by_design', 'cloud placeholder'),
            group('/private/var/folders', 1, 'timeout', 'operation timed out after 1s'),
            group('/home', 1, 'by_design', 'autofs mount'),
        ],
        skip_other: null,
    },
};

test('a truncated report exposes the budget and the missing total', () => {
    const view = coverageView(live);
    assert.ok(view);
    assert.equal(view.total, 490);
    assert.equal(view.groupCount, 8);
    assert.equal(view.legacy, false);
    assert.equal(view.truncated, true);
    assert.equal(budgetClock(view.budgetSeconds), '10:00');
    assert.equal(budgetWords(view.budgetSeconds), '10 min');
});

test('the inline list shows four groups and counts the rest exactly', () => {
    const view = coverageView(live);
    assert.equal(inlineGroups(view).length, 4);
    assert.equal(inlineRest(view), '4 more groups (49 dirs) · 7 by design');
    const shown = inlineGroups(view).reduce((sum, g) => sum + g.count, 0);
    assert.equal(shown + 49, view.total);
});

test('kinds drive both the label and the tag colour', () => {
    assert.equal(skipKindLabel('permission'), 'permission');
    assert.equal(skipKindLabel('timeout'), 'timed out');
    assert.equal(skipKindLabel('io'), 'read error');
    assert.equal(skipKindLabel('by_design'), 'by design');
    assert.equal(skipKindClass('permission'), 'perm');
    assert.equal(skipKindClass('timeout'), 'time');
    assert.equal(skipKindClass('io'), 'io');
    assert.equal(skipKindClass('by_design'), 'byconf');
    assert.equal(skipKindClass(undefined), 'io');
});

test('permission and by-design totals never mix', () => {
    const view = coverageView(live);
    assert.equal(view.permissionDirs, 379);
    assert.equal(view.byDesignDirs, 7);
    assert.equal(showsFullDiskAccessHint(view), true);
    assert.equal(showsFullDiskAccessHint({ ...view, permissionDirs: 19 }), false);
});

test('a group row reads as reason plus one example', () => {
    assert.equal(
        groupDetail(group('~/Library/Containers', 3, 'permission', 'operation not permitted', '…/com.apple.mail')),
        'operation not permitted · e.g. …/com.apple.mail',
    );
    assert.equal(groupDetail(group('~', 1, 'io')), '');
});

test('core folders read as a fraction, not a boolean', () => {
    assert.equal(priorityFraction(live), '3/6');
    assert.equal(partialNote(live), ' · partial 3/6 · 10 min budget');
    assert.equal(rootStateMark('done'), '✓');
    assert.equal(rootStateMark('partial'), '◔');
    assert.equal(rootStateMark('not_started'), '○');
    assert.equal(rootStateLabel('not_started'), 'not started');
});

test('a complete walk with nothing skipped has no coverage view at all', () => {
    const clean = {
        ...live,
        not_counted_total: 0,
        no_access_dirs: 0,
        truncated: false,
        coverage: {
            ...live.coverage,
            priority_scanned: 6,
            priority_complete: true,
            truncated: false,
            not_counted_total: 0,
            skip_groups: [],
            skip_other: null,
        },
    };
    assert.equal(coverageView(clean), null);
    assert.equal(partialNote(clean), '');
});

test('an old report keeps a count-only view instead of disappearing', () => {
    const old = {
        id: '20250101-000000',
        complete: false,
        truncated: true,
        no_access_dirs: 432,
        scanned_at: '2025-01-01T00:00:00Z',
        categories: [],
        scan_ms: 300_039,
        files: 1,
        scanned_bytes: 1,
    };
    const view = coverageView(old);
    assert.ok(view);
    assert.equal(view.total, 432);
    assert.equal(view.legacy, true);
    assert.equal(view.groupCount, 0);
    assert.equal(inlineRest(view), '');
    assert.equal(partialNote(old), ' · partial');
});

test('the copy list stays plain text and covers every row', () => {
    const lines = coverageText(coverageView(live)).split('\n');
    assert.equal(lines.length, 9);
    assert.match(lines[0], /^490 folders not counted, 8 groups · walk hit the 10 min budget$/);
    assert.equal(lines[1], '283\t~/Library/Containers\tpermission\toperation not permitted');
    assert.equal(lines[8], '1\t/home\tby design\tautofs mount');
});

test('a capped report folds the tail into skip_other without losing the total', () => {
    const capped = {
        ...live,
        coverage: {
            ...live.coverage,
            skip_groups: live.coverage.skip_groups.slice(0, 4),
            skip_other: { groups: 4, dirs: 49 },
        },
    };
    const view = coverageView(capped);
    assert.equal(view.groupCount, 8);
    // The by-design rows moved into skip_other, so their kind is unknown here;
    // claiming "7 by design" would invent information the server did not keep.
    assert.equal(inlineRest(view), '4 more groups (49 dirs)');
});
