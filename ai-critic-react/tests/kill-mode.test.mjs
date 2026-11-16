import assert from 'node:assert/strict';
import { test } from 'node:test';
import {
    KILL_MODES,
    killModeConfirmLabel,
    killModeSignal,
    killModeSignalText,
    killModeTitle,
    resolveBatchKillModal,
    resolveKillModal,
} from '../src/v2/mcc/home/killMode.ts';

test('graceful is the default kill mode', () => {
    assert.deepEqual([...KILL_MODES], ['graceful', 'force']);
    assert.equal(killModeSignal('graceful'), 'TERM');
    assert.equal(killModeSignal('force'), 'KILL');
    assert.equal(killModeTitle('graceful'), 'Graceful');
    assert.equal(killModeTitle('force'), 'Force Quit');
    assert.equal(killModeSignalText('graceful'), 'SIGTERM · 15');
    assert.equal(killModeSignalText('force'), 'SIGKILL · 9');
    assert.equal(killModeConfirmLabel('graceful'), 'Kill Process');
    assert.equal(killModeConfirmLabel('force'), 'Force Quit');
});

test('graceful mode keeps the existing TERM command and copy', () => {
    const state = resolveKillModal({ pid: 4629, mode: 'graceful' });
    assert.equal(state.signal, 'TERM');
    assert.equal(state.command, 'kill -TERM 4629');
    assert.equal(state.confirmLabel, 'Kill Process');
    assert.equal(state.message, 'Are you sure you want to kill this process?');
    assert.equal(state.warning, undefined);
    assert.equal(state.locked, false);
});

test('force mode switches command, label, message and warns about data loss', () => {
    const state = resolveKillModal({ pid: 4629, mode: 'force' });
    assert.equal(state.signal, 'KILL');
    assert.equal(state.command, 'kill -KILL 4629');
    assert.equal(state.confirmLabel, 'Force Quit');
    assert.match(state.message, /force quit/i);
    assert.match(state.warning ?? '', /SIGKILL/);
    assert.match(state.warning ?? '', /unsaved work/i);
    assert.equal(state.locked, false);
});

test('protocol processes stay locked in both modes', () => {
    for (const mode of KILL_MODES) {
        const state = resolveKillModal({ pid: 178, mode, protectedReason: 'system-critical (WindowServer)' });
        assert.equal(state.locked, true);
        assert.equal(state.command, 'kill 178');
        assert.equal(state.confirmLabel, 'Cannot Kill');
        assert.equal(state.signal, 'TERM');
        assert.equal(state.warning, 'system-critical (WindowServer)');
    }
});

test('tunnel warning survives both modes', () => {
    const graceful = resolveKillModal({ pid: 900, mode: 'graceful', isTunnel: true });
    assert.match(graceful.warning ?? '', /Cloudflare connector/);
    assert.doesNotMatch(graceful.warning ?? '', /SIGKILL/);

    const force = resolveKillModal({ pid: 900, mode: 'force', isTunnel: true });
    assert.match(force.warning ?? '', /Cloudflare connector/);
    assert.match(force.warning ?? '', /SIGKILL/);
});

test('batch kill lists one command per pid and labels the count', () => {
    const state = resolveBatchKillModal({ pids: [11, 22], mode: 'graceful' });
    assert.equal(state.signal, 'TERM');
    assert.equal(state.command, 'kill -TERM 11\nkill -TERM 22');
    assert.equal(state.confirmLabel, 'Kill 2');
    assert.match(state.message, /2 processes/);
    assert.equal(state.locked, false);
});

test('batch force quit uses SIGKILL on every pid', () => {
    const state = resolveBatchKillModal({ pids: [11, 22, 33], mode: 'force' });
    assert.equal(state.signal, 'KILL');
    assert.match(state.command, /kill -KILL 33/);
    assert.equal(state.confirmLabel, 'Force Quit 3');
});

test('an empty batch cannot confirm', () => {
    const state = resolveBatchKillModal({ pids: [], mode: 'graceful' });
    assert.equal(state.locked, true);
    assert.equal(state.confirmLabel, 'Cannot Kill');
});
