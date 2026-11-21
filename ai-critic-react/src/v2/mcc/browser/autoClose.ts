import type { AutoCloseRecord, AutoCloseStatus } from './browserWire';

/**
 * The idle-window rule is the one browser feature that acts while nobody is
 * looking, so its wording lives here as pure functions: the rail, the dialog
 * and the record have to say the same thing about a threshold, a countdown and
 * an idle duration, and a unit test can pin all three.
 */

export const AUTO_CLOSE_DEFAULT_SECONDS = 3600;
export const AUTO_CLOSE_MIN_SECONDS = 300;
export const AUTO_CLOSE_MAX_SECONDS = 86400;
export const AUTO_CLOSE_RECORD_LIMIT = 10;

/** The two fields every rail row already has; both surfaces share the wording. */
export interface AutoCloseRow {
    current?: boolean;
    idle_seconds?: number;
}

/** The presets the dialog offers before "Custom". In minutes. */
export const AUTO_CLOSE_PRESETS = [15, 30, 60, 120] as const;

export function clampIdleSeconds(seconds: number): number {
    if (!Number.isFinite(seconds) || seconds <= 0) return AUTO_CLOSE_DEFAULT_SECONDS;
    return Math.min(AUTO_CLOSE_MAX_SECONDS, Math.max(AUTO_CLOSE_MIN_SECONDS, Math.round(seconds)));
}

/** "1 hour", "90 minutes" — the sentence shown beside the threshold. */
export function idleLabel(seconds: number): string {
    const minutes = Math.round(seconds / 60);
    if (minutes < 60) return `${minutes} ${minutes === 1 ? 'minute' : 'minutes'}`;
    const hours = Math.floor(minutes / 60);
    const rest = minutes % 60;
    if (rest) return `${hours}h ${rest}m`;
    return `${hours} ${hours === 1 ? 'hour' : 'hours'}`;
}

/**
 * Short form for chips and footer copy: "1h", "1h 5m", "45m". Kept separate from
 * idleLabel because a rail row has no room for the sentence form.
 */
export function idleShort(seconds: number): string {
    const total = Math.max(0, Math.round(seconds / 60));
    const h = Math.floor(total / 60);
    const m = total % 60;
    if (h && m) return `${h}h ${m}m`;
    if (h) return `${h}h`;
    return `${m}m`;
}

/** The preset a saved interval maps to, or 'custom' when it is not one. */
export function presetOf(seconds: number): number | 'custom' {
    const minutes = seconds / 60;
    return AUTO_CLOSE_PRESETS.includes(minutes as (typeof AUTO_CLOSE_PRESETS)[number])
        ? minutes
        : 'custom';
}

export function secondsOfPreset(minutes: number): number {
    return clampIdleSeconds(minutes * 60);
}

/** A rail chip for one window, or '' when the window needs no explanation. */
export function idleChipLabel(row: AutoCloseRow, thresholdSeconds: number, enabled: boolean): string {
    if (row.current) return 'in use';
    const idle = row.idle_seconds ?? 0;
    if (!idle) return '';
    if (!enabled) return `idle ${idleShort(idle)}`;
    if (idle >= thresholdSeconds) return 'closing now';
    const left = thresholdSeconds - idle;
    return left <= 600 ? `closes in ${Math.ceil(left / 60)}m` : `idle ${idleShort(idle)}`;
}

/**
 * The footer's countdown: the closest window to the chopping block. Returns ''
 * when nothing is being watched, so the caller can keep the rail quiet.
 */
export function nextCloseLabel(rows: AutoCloseRow[], thresholdSeconds: number, enabled: boolean): string {
    if (!enabled) return 'windows stay open';
    let highest = 0;
    for (const row of rows) {
        if (row.current) continue;
        highest = Math.max(highest, row.idle_seconds ?? 0);
    }
    if (!highest) return 'watching';
    if (highest >= thresholdSeconds) return 'closing now';
    return `closes in ${Math.ceil((thresholdSeconds - highest) / 60)}m`;
}

/** Why the rule cannot be armed, or '' when it can. */
export function autoCloseBlocker(opts: {
    supported: boolean;
    readOnly: boolean;
    busy: boolean;
    status: AutoCloseStatus | null;
}): string {
    if (!opts.supported) return 'Reload browser-agent 1.0.22+ to close windows';
    if (opts.readOnly) return 'Read-only: this daemon cannot control windows';
    if (!opts.status) return 'Waiting for the daemon';
    if (opts.busy) return 'Wait for the current action';
    return '';
}

/** "just now", "4m ago", "1h 12m ago" for a record timestamp. */
export function recordAgo(closedAt: string, now = Date.now()): string {
    const at = Date.parse(closedAt);
    if (Number.isNaN(at)) return '';
    const seconds = Math.max(0, Math.round((now - at) / 1000));
    if (seconds < 30) return 'just now';
    const minutes = Math.floor(seconds / 60);
    if (minutes < 60) return `${minutes}m ago`;
    const hours = Math.floor(minutes / 60);
    const rest = minutes % 60;
    return rest ? `${hours}h ${rest}m ago` : `${hours}h ago`;
}

/** The record's own sentence: what was idle for how long, with how many tabs. */
export function recordSummary(record: AutoCloseRecord): string {
    const tabs = `${record.tabs} ${record.tabs === 1 ? 'tab' : 'tabs'}`;
    return `${tabs} · idle ${idleShort(record.idle_seconds)}`;
}

/** One-line toast after a close; the session id is the fallback title. */
export function autoCloseToast(records: AutoCloseRecord[]): string {
    if (records.length === 0) return '';
    const totalTabs = records.reduce((sum, record) => sum + record.tabs, 0);
    const windows = records.length === 1 ? '1 idle window' : `${records.length} idle windows`;
    return `Closed ${windows} · ${totalTabs} ${totalTabs === 1 ? 'tab' : 'tabs'}`;
}

/** Draft seconds for a custom value in the dialog; invalid drafts keep the old value. */
export function customSeconds(value: number, unit: 'minutes' | 'hours'): number {
    if (!Number.isFinite(value) || value <= 0) return AUTO_CLOSE_DEFAULT_SECONDS;
    return clampIdleSeconds(unit === 'hours' ? value * 3600 : value * 60);
}
