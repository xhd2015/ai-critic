/**
 * Pure decision logic for the System → Kill Process modal.
 *
 * Kept free of React so tests can assert every copy/command combination without
 * a DOM. `SystemKillModal` renders exactly what these helpers return.
 */

export type KillSignal = 'TERM' | 'KILL';
export type KillMode = 'graceful' | 'force';

export const KILL_MODES: readonly KillMode[] = ['graceful', 'force'];

const TUNNEL_WARNING =
    'This looks like the Cloudflare connector. Killing it disconnects this page until the tunnel respawns.';

const FORCE_WARNING =
    'Force Quit sends SIGKILL. The process cannot intercept it, cleanup handlers do not run, and unsaved work may be lost.';

export function killModeSignal(mode: KillMode): KillSignal {
    return mode === 'force' ? 'KILL' : 'TERM';
}

export function killModeTitle(mode: KillMode): string {
    return mode === 'force' ? 'Force Quit' : 'Graceful';
}

export function killModeSignalText(mode: KillMode): string {
    return mode === 'force' ? 'SIGKILL · 9' : 'SIGTERM · 15';
}

export function killModeConfirmLabel(mode: KillMode): string {
    return mode === 'force' ? 'Force Quit' : 'Kill Process';
}

export interface KillModalState {
    /** Signal sent to `/api/server/processes/kill`. Always TERM when locked. */
    signal: KillSignal;
    /** Shell preview shown under "Command to execute". */
    command: string;
    confirmLabel: string;
    message: string;
    /** Replaces `message` in the shared modal when set. */
    warning?: string;
    /** Protected processes ignore the selected mode entirely. */
    locked: boolean;
}

export interface KillModalInput {
    pid: number;
    mode: KillMode;
    /** Reason string from `HostProcess.protected`; presence means "cannot kill". */
    protectedReason?: string;
    isTunnel?: boolean;
}

/**
 * Single source of truth for every string in the kill modal, so switching the
 * mode cannot leave the command preview and the button label out of sync.
 */
export function resolveKillModal({ pid, mode, protectedReason, isTunnel }: KillModalInput): KillModalState {
    if (protectedReason) {
        return {
            signal: 'TERM',
            command: `kill ${pid}`,
            confirmLabel: 'Cannot Kill',
            message: 'This process is protected and cannot be killed from the UI.',
            warning: protectedReason,
            locked: true,
        };
    }

    const warnings = [mode === 'force' ? FORCE_WARNING : undefined, isTunnel ? TUNNEL_WARNING : undefined].filter(
        (value): value is string => !!value,
    );

    return {
        signal: killModeSignal(mode),
        command: `kill -${killModeSignal(mode)} ${pid}`,
        confirmLabel: killModeConfirmLabel(mode),
        message:
            mode === 'force'
                ? 'Are you sure you want to force quit this process?'
                : 'Are you sure you want to kill this process?',
        warning: warnings.length > 0 ? warnings.join(' ') : undefined,
        locked: false,
    };
}
