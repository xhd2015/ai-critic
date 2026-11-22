import assert from 'node:assert/strict';
import { test } from 'node:test';
import {
    AUTO_CLOSE_DEFAULT_SECONDS,
    AUTO_CLOSE_NEXT_LIMIT,
    autoCloseRows,
    dueLabel,
    nextTen,
    nextTenCount,
    AUTO_CLOSE_MAX_SECONDS,
    AUTO_CLOSE_MIN_SECONDS,
    autoCloseBlocker,
    autoCloseToast,
    clampIdleSeconds,
    customSeconds,
    idleChipLabel,
    idleLabel,
    idleShort,
    nextCloseLabel,
    presetOf,
    recordAgo,
    recordSummary,
    secondsOfPreset,
} from '../src/v2/mcc/browser/autoClose.ts';

test('the interval never leaves the 5 minute – 24 hour range', () => {
    assert.equal(clampIdleSeconds(0), AUTO_CLOSE_DEFAULT_SECONDS);
    assert.equal(clampIdleSeconds(Number.NaN), AUTO_CLOSE_DEFAULT_SECONDS);
    assert.equal(clampIdleSeconds(120), AUTO_CLOSE_MIN_SECONDS);
    assert.equal(clampIdleSeconds(999999), AUTO_CLOSE_MAX_SECONDS);
    assert.equal(clampIdleSeconds(3600), 3600);
});

test('idle duration reads as a sentence and as a chip', () => {
    assert.equal(idleLabel(3600), '1 hour');
    assert.equal(idleLabel(7200), '2 hours');
    assert.equal(idleLabel(5400), '1h 30m');
    assert.equal(idleLabel(900), '15 minutes');
    assert.equal(idleShort(3600), '1h');
    assert.equal(idleShort(3900), '1h 5m');
    assert.equal(idleShort(300), '5m');
});

test('saved intervals map back to the preset that produced them', () => {
    assert.equal(presetOf(3600), 60);
    assert.equal(presetOf(900), 15);
    assert.equal(presetOf(5400), 'custom');
    assert.equal(secondsOfPreset(15), 900);
    assert.equal(secondsOfPreset(1), AUTO_CLOSE_MIN_SECONDS);
});

test('a window chip explains the next close without jumping the gun', () => {
    // No exemptions: even the window being viewed gets a real countdown.
    assert.equal(idleChipLabel({ current: true, idle_seconds: 7200 }, 3600, true), 'closing now');
    assert.equal(idleChipLabel({ idle_seconds: 0 }, 3600, true), '');
    assert.equal(idleChipLabel({ idle_seconds: 600 }, 3600, false), 'idle 10m');
    assert.equal(idleChipLabel({ idle_seconds: 3600 }, 3600, true), 'closing now');
    assert.equal(idleChipLabel({ idle_seconds: 3480 }, 3600, true), 'closes in 2m');
    assert.equal(idleChipLabel({ idle_seconds: 600 }, 3600, true), 'idle 10m');
});

test('the footer countdown names the closest window, and stays quiet when off', () => {
    const rows = [
        { current: true, idle_seconds: 9999 },
        { idle_seconds: 3480 },
        { idle_seconds: 600 },
    ];
    assert.equal(nextCloseLabel(rows, 3600, true), 'closing now');
    assert.equal(nextCloseLabel(rows, 3600, false), 'windows stay open');
    assert.equal(nextCloseLabel([{ current: true, idle_seconds: 9999 }], 3600, true), 'closing now');
    assert.equal(nextCloseLabel([], 3600, true), 'watching');
    assert.equal(nextCloseLabel([{ idle_seconds: 3700 }], 3600, true), 'closing now');
});

test('the rule explains the first blocker in priority order', () => {
    assert.match(
        autoCloseBlocker({ supported: false, readOnly: true, busy: true, status: null }),
        /1\.0\.22/,
    );
    assert.match(
        autoCloseBlocker({ supported: true, readOnly: true, busy: true, status: null }),
        /Read-only/,
    );
    assert.match(
        autoCloseBlocker({ supported: true, readOnly: false, busy: true, status: null }),
        /Waiting/,
    );
    assert.match(
        autoCloseBlocker({
            supported: true,
            readOnly: false,
            busy: true,
            status: { enabled: false, idle_seconds: 3600, supported: true, records: [] },
        }),
        /Wait/,
    );
    assert.equal(
        autoCloseBlocker({
            supported: true,
            readOnly: false,
            busy: false,
            status: { enabled: false, idle_seconds: 3600, supported: true, records: [] },
        }),
        '',
    );
});

test('a record timestamp turns into a relative age', () => {
    const now = Date.parse('2026-11-21T12:00:00Z');
    assert.equal(recordAgo('2026-11-21T12:00:00Z', now), 'just now');
    assert.equal(recordAgo('2026-11-21T11:56:00Z', now), '4m ago');
    assert.equal(recordAgo('2026-11-21T10:48:00Z', now), '1h 12m ago');
    assert.equal(recordAgo('2026-11-20T21:00:00Z', now), '15h ago');
    assert.equal(recordAgo('not a timestamp', now), '');
});

test('the record names what closed and how long it had been idle', () => {
    const record = { id: 1, window_id: 910, tabs: 4, idle_seconds: 3600, closed_at: '2026-11-21T12:00:00Z' };
    assert.equal(recordSummary(record), '4 tabs · idle 1h');
    assert.equal(autoCloseToast([record]), 'Closed 1 idle window · 4 tabs');
    assert.equal(
        autoCloseToast([record, { ...record, id: 2, tabs: 2 }]),
        'Closed 2 idle windows · 6 tabs',
    );
    assert.equal(autoCloseToast([]), '');
});

test('a custom draft is clamped before it can become a rule', () => {
    assert.equal(customSeconds(90, 'minutes'), 5400);
    assert.equal(customSeconds(2, 'hours'), 7200);
    assert.equal(customSeconds(0, 'minutes'), AUTO_CLOSE_DEFAULT_SECONDS);
    assert.equal(customSeconds(0.01, 'minutes'), AUTO_CLOSE_MIN_SECONDS);
});


test('Next 10 ranks oldest activity first and never exceeds ten', () => {
    const rows = [
        { key: 'current', current: true, idle_seconds: 9999 },
        { key: 'busy', busy: true, idle_seconds: 9998 },
        { key: 'a', idle_seconds: 60, tabCount: 2 },
        { key: 'b', idle_seconds: 7200, tabCount: 4 },
        { key: 'c', idle_seconds: 1800, tabCount: 1 },
        { key: 'd', idle_seconds: 7200, tabCount: 3 },
        { key: 'e', idle_seconds: 3000 },
        { key: 'f', idle_seconds: 2400 },
        { key: 'g', idle_seconds: 600 },
        { key: 'h', idle_seconds: 4800 },
        { key: 'i', idle_seconds: 3600 },
        { key: 'j', idle_seconds: 1200 },
        { key: 'k', idle_seconds: 60 },
    ];
    const plan = nextTen(rows);
    assert.equal(plan.length, AUTO_CLOSE_NEXT_LIMIT);
    // Nothing is exempt: the viewed and busy windows rank like the rest.
    // Ties break on key so two renders cannot disagree.
    assert.deepEqual(
        plan.map((row) => row.key),
        ['current', 'busy', 'b', 'd', 'h', 'i', 'e', 'f', 'c', 'j'],
    );
    assert.ok(plan.every((row) => row.idleSeconds >= plan[plan.length - 1].idleSeconds));
});

test('Next 10 ranks the viewed and busy windows instead of dropping them', () => {
    const plan = nextTen([
        { key: 'current', current: true, idle_seconds: 7200 },
        { key: 'busy', busy: true, idle_seconds: 7200 },
        { key: 'idle', idle_seconds: 7200, tabCount: 2 },
    ]);
    assert.deepEqual(plan.map((row) => row.key), ['busy', 'current', 'idle']);
});

test('Next 10 copes with fewer windows than the cap, and with none', () => {
    assert.deepEqual(nextTen([{ key: 'only', idle_seconds: 300 }]).map((row) => row.key), ['only']);
    assert.deepEqual(
        nextTen([{ key: 'current', current: true, idle_seconds: 300 }]).map((row) => row.key),
        ['current'],
    );
    assert.deepEqual(nextTen([]), []);
    assert.equal(nextTen([], 0).length, 0);
});

test('a due label separates "now" from a countdown', () => {
    assert.deepEqual(dueLabel(3600, 3600), { tone: 'now', text: 'due now' });
    assert.deepEqual(dueLabel(4200, 3600), { tone: 'now', text: 'due now' });
    assert.deepEqual(dueLabel(3480, 3600), { tone: 'soon', text: 'in 2m' });
    assert.deepEqual(dueLabel(3000, 3600), { tone: 'soon', text: 'in 10m' });
    assert.deepEqual(dueLabel(2400, 3600), { tone: 'later', text: 'in 20m' });
    assert.deepEqual(dueLabel(0, 3600), { tone: 'later', text: 'in 1h' });
});

test('the section count explains the cap', () => {
    assert.equal(nextTenCount(11, 10), '10 of 11 · oldest first');
    assert.equal(nextTenCount(3, 3), '3 of 3 · oldest first');
    assert.equal(nextTenCount(0, 0), 'no other windows');
});

test('sessions become rows with the live tab count for the current window', () => {
    const rows = autoCloseRows(
        [
            { id: 'live', title: 'Docs', idle_seconds: 120, tabs: [{ tab_id: 1 }, { tab_id: 2 }] },
            { id: 'current', title: '', idle_seconds: 0 },
        ],
        [{ tab_id: 9 }, { tab_id: 10 }, { tab_id: 11 }],
        'current',
    );
    assert.deepEqual(rows[0], { key: 'live', title: 'Docs', tabCount: 2, current: false, idle_seconds: 120 });
    assert.deepEqual(rows[1], { key: 'current', title: 'current', tabCount: 3, current: true, idle_seconds: 0 });
});
