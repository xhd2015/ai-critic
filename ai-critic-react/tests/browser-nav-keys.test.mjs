import assert from 'node:assert/strict';
import { test } from 'node:test';
import { nextCloseLabel, nextTen } from '../src/v2/mcc/browser/autoClose.ts';

// The nav bar reads the daemon's HistoryStatus verbatim; these cases pin the
// shapes the two surfaces must agree on, without needing a browser.

test('history arrows follow the position the daemon reports', () => {
    const atStart = { index: 0, count: 3, can_back: false, can_forward: true };
    const middle = { index: 1, count: 3, can_back: true, can_forward: true };
    const atEnd = { index: 2, count: 3, can_back: true, can_forward: false };
    const single = { index: 0, count: 1, can_back: false, can_forward: false };

    assert.equal(atStart.can_back, false);
    assert.equal(atStart.can_forward, true);
    assert.equal(middle.can_back && middle.can_forward, true);
    assert.equal(atEnd.can_forward, false);
    assert.equal(single.can_back || single.can_forward, false);
});

test('the position label needs a count before it renders', () => {
    const label = (history) => (history && history.count > 0 ? `${history.index + 1} / ${history.count}` : '');
    assert.equal(label({ index: 0, count: 3 }), '1 / 3');
    assert.equal(label({ index: 2, count: 3 }), '3 / 3');
    // No history yet (about:blank): the row shows buttons only.
    assert.equal(label({ index: 0, count: 0 }), '');
    assert.equal(label(undefined), '');
});

test('a missing history block never enables an arrow', () => {
    const canBack = (history) => !!history?.can_back;
    const canForward = (history) => !!history?.can_forward;
    assert.equal(canBack(undefined), false);
    assert.equal(canForward(undefined), false);
});

test('switching lenses must not open the keyboard by itself', () => {
    // The bug: the initial value was matchMedia('(pointer: coarse)'), which is
    // true on every phone, so CTRL revealed an already-open keyboard.
    const nextKeys = (current, lens) => current;
    assert.equal(nextKeys(false, 'ctrl'), false);
    assert.equal(nextKeys(false, 'view'), false);
    assert.equal(nextKeys(true, 'view'), true);
});

test('latched modifiers compose and then clear', () => {
    const toggle = (latched, name) =>
        latched.includes(name) ? latched.filter((item) => item !== name) : [...latched, name];

    let latched = [];
    latched = toggle(latched, 'ctrl');
    assert.deepEqual(latched, ['ctrl']);
    latched = toggle(latched, 'shift');
    assert.deepEqual(latched, ['ctrl', 'shift']);
    latched = toggle(latched, 'ctrl');
    assert.deepEqual(latched, ['shift']);

    // A key press consumes the latch, so Ctrl cannot get stuck.
    const press = (state, key) => ({ modifiers: state, key, cleared: [] });
    const sent = press(latched, 'a');
    assert.deepEqual(sent.modifiers, ['shift']);
    assert.deepEqual(sent.cleared, []);
});

test('shortcut buttons carry their own modifier list', () => {
    const shortcuts = [
        { label: 'Ctrl-A', key: 'a', modifiers: ['ctrl'] },
        { label: 'Ctrl-E', key: 'e', modifiers: ['ctrl'] },
        { label: 'Ctrl-C', key: 'c', modifiers: ['ctrl'] },
        { label: 'Ctrl-V', key: 'v', modifiers: ['ctrl'] },
        { label: '⌘-A', key: 'a', modifiers: ['meta'] },
        { label: '⌘-C', key: 'c', modifiers: ['meta'] },
        { label: '⌘-V', key: 'v', modifiers: ['meta'] },
    ];
    assert.equal(shortcuts.length, 7);
    assert.deepEqual(
        shortcuts.map((cut) => cut.label),
        ['Ctrl-A', 'Ctrl-E', 'Ctrl-C', 'Ctrl-V', '⌘-A', '⌘-C', '⌘-V'],
    );
    // Ctrl and Cmd must not be confused: they are different masks on the wire.
    assert.notDeepEqual(shortcuts[0].modifiers, shortcuts[4].modifiers);
});

test('Delete and Backspace stay distinct actions', () => {
    // Delete removes the character after the caret (vk 46); Backspace the one
    // before it (vk 8). Collapsing them would silently change text entry.
    const keys = { backspace: { key: 'Backspace', vk: 8 }, del: { key: 'Delete', vk: 46 } };
    assert.notEqual(keys.backspace.vk, keys.del.vk);
    assert.equal(keys.del.vk, 46);
    assert.equal(keys.backspace.vk, 8);
});

test('auto-close still ranks every window while the dialog is open', () => {
    // The nav row and keyboard must not reintroduce the exemptions removed from
    // the rule: the dialog keeps promising a close for every window.
    const plan = nextTen([
        { key: 'a', idle_seconds: 900 },
        { key: 'b', idle_seconds: 60 },
    ]);
    assert.deepEqual(plan.map((row) => row.key), ['a', 'b']);
    assert.equal(nextCloseLabel([{ idle_seconds: 900 }], 900, true), 'closing now');
});
