import assert from 'node:assert/strict';
import { test } from 'node:test';
import {
    APP_SORTS,
    appInitials,
    appIsRunning,
    applyLockedAppOrder,
    appStatusText,
    filterApps,
    orderedAppPaths,
    sortApps,
} from '../src/v2/mcc/home/appFilters.ts';

const app = (name, over = {}) => ({
    name,
    path: `/Applications/${name}.app`,
    process_count: 0,
    cpu_percent: 0,
    rss_bytes: 0,
    ...over,
});

test('filterApps hides system bundles unless asked', () => {
    const apps = [
        app('Safari', { system: true }),
        app('Slack', { version: '4.41.96' }),
        app('Notes', { system: true, bundle_id: 'com.apple.Notes' }),
    ];
    const hidden = filterApps(apps, '', false).map(a => a.name);
    const shown = filterApps(apps, '', true).map(a => a.name);
    assert.deepEqual(hidden, ['Slack']);
    assert.deepEqual(shown, ['Safari', 'Slack', 'Notes']);
});

test('filterApps matches name, version, bundle id and path, case-insensitively', () => {
    const apps = [
        app('Google Chrome', { version: '157.0.8103.79', bundle_id: 'com.google.Chrome' }),
        app('Xcode', { version: '16.2' }),
    ];
    assert.deepEqual(filterApps(apps, 'chrome').map(a => a.name), ['Google Chrome']);
    assert.deepEqual(filterApps(apps, '157.0').map(a => a.name), ['Google Chrome']);
    assert.deepEqual(filterApps(apps, 'com.google').map(a => a.name), ['Google Chrome']);
    assert.deepEqual(filterApps(apps, 'xcode.app').map(a => a.name), ['Xcode']);
    assert.deepEqual(filterApps(apps, '   ').map(a => a.name), ['Google Chrome', 'Xcode']);
    assert.deepEqual(filterApps(apps, 'nothing-here'), []);
});

test('sortApps keeps running apps first, then sorts each half', () => {
    const apps = [
        app('Zed', { process_count: 3, cpu_percent: 5, rss_bytes: 100 }),
        app('Idle Heavy', { rss_bytes: 9000 }),
        app('Chrome', { process_count: 185, cpu_percent: 42.5, rss_bytes: 5583 }),
        app('Idle Light'),
    ];
    assert.deepEqual(sortApps(apps, 'cpu').map(a => a.name), ['Chrome', 'Zed', 'Idle Heavy', 'Idle Light']);
    assert.deepEqual(sortApps(apps, 'mem').map(a => a.name), ['Chrome', 'Zed', 'Idle Heavy', 'Idle Light']);
    assert.deepEqual(sortApps(apps, 'name').map(a => a.name), ['Chrome', 'Zed', 'Idle Heavy', 'Idle Light']);
    // input order is untouched
    assert.deepEqual(apps.map(a => a.name), ['Zed', 'Idle Heavy', 'Chrome', 'Idle Light']);
});

test('sortApps name puts running apps first regardless of alphabetical order', () => {
    const apps = [app('Alpha'), app('Zulu', { process_count: 1 })];
    assert.deepEqual(sortApps(apps, 'name').map(a => a.name), ['Zulu', 'Alpha']);
});

test('locked order wins and new apps keep their sorted place at the end', () => {
    const apps = [app('A'), app('B'), app('C')];
    const locked = orderedAppPaths([apps[2], apps[0]]);
    assert.deepEqual(applyLockedAppOrder(apps, locked).map(a => a.name), ['C', 'A', 'B']);
    assert.deepEqual(applyLockedAppOrder(apps, []).map(a => a.name), ['A', 'B', 'C']);
    assert.deepEqual(applyLockedAppOrder(apps, null).map(a => a.name), ['A', 'B', 'C']);
    // a stale path (app uninstalled) is ignored instead of crashing
    assert.deepEqual(applyLockedAppOrder(apps, ['/Applications/Gone.app']).map(a => a.name), ['A', 'B', 'C']);
});

test('appStatusText reads like a sentence', () => {
    assert.equal(appStatusText(app('A')), 'not running');
    assert.equal(appStatusText(app('A', { process_count: 1 })), '1 proc · running');
    assert.equal(appStatusText(app('A', { process_count: 12 })), '12 procs · running');
    assert.equal(appIsRunning(app('A')), false);
    assert.equal(appIsRunning(app('A', { process_count: 2 })), true);
});

test('appInitials always yields one or two letters', () => {
    assert.equal(appInitials('Google Chrome'), 'GC');
    assert.equal(appInitials('Xcode'), 'XC');
    assert.equal(appInitials('zoom.us'), 'ZU');
    assert.equal(appInitials('ai-critic-macos'), 'AI');
    assert.equal(appInitials('  '), '?');
});

test('APP_SORTS drives the CPU / Memory / Name chips without PID', () => {
    assert.deepEqual(APP_SORTS.map(([id]) => id), ['cpu', 'mem', 'name']);
    assert.deepEqual(APP_SORTS.map(([, label]) => label), ['CPU ↓', 'Memory', 'Name']);
});
