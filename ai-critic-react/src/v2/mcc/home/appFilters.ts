import type { InstalledApp } from '../../../api/apps';

export type AppSort = 'cpu' | 'mem' | 'name';

export const APP_SORTS: readonly (readonly [AppSort, string])[] = [
    ['cpu', 'CPU ↓'],
    ['mem', 'Memory'],
    ['name', 'Name'],
];

export function appIsRunning(app: InstalledApp): boolean {
    return (app.process_count ?? 0) > 0;
}

export function filterApps(apps: InstalledApp[], query: string, showSystem: boolean): InstalledApp[] {
    const needle = query.trim().toLowerCase();
    return apps.filter(app => {
        if (!showSystem && app.system) return false;
        if (!needle) return true;
        return [app.name, app.version ?? '', app.bundle_id ?? '', app.exec ?? '', app.path].some(value =>
            value.toLowerCase().includes(needle),
        );
    });
}

/**
 * Running apps always come first — an idle bundle is background context, not a
 * result — then each half is sorted by the selected key.
 */
export function sortApps(apps: InstalledApp[], sort: AppSort): InstalledApp[] {
    const compare = (a: InstalledApp, b: InstalledApp): number => {
        switch (sort) {
            case 'name':
                return a.name.localeCompare(b.name);
            case 'mem':
                return b.rss_bytes - a.rss_bytes || b.cpu_percent - a.cpu_percent;
            default:
                return b.cpu_percent - a.cpu_percent || b.rss_bytes - a.rss_bytes;
        }
    };
    const running = apps.filter(appIsRunning).sort(compare);
    const idle = apps.filter(app => !appIsRunning(app)).sort(compare);
    return [...running, ...idle];
}

export function orderedAppPaths(apps: InstalledApp[]): string[] {
    return apps.map(app => app.path);
}

export function applyLockedAppOrder(apps: InstalledApp[], locked: string[] | null | undefined): InstalledApp[] {
    if (!locked || locked.length === 0) return apps;
    const byPath = new Map(apps.map(app => [app.path, app]));
    const out: InstalledApp[] = [];
    for (const path of locked) {
        const app = byPath.get(path);
        if (app) {
            out.push(app);
            byPath.delete(path);
        }
    }
    return out.concat(Array.from(byPath.values()));
}

/** `2 procs · running` or `not running`. */
export function appStatusText(app: InstalledApp): string {
    const count = app.process_count ?? 0;
    if (count === 0) return 'not running';
    return `${count} proc${count === 1 ? '' : 's'} · running`;
}

/** Two-letter tile for apps that ship no icon (or a non-PNG one). */
export function appInitials(name: string): string {
    const words = name
        .replace(/[._-]+/g, ' ')
        .split(/\s+/)
        .filter(Boolean);
    if (words.length === 0) return '?';
    // `ai-critic-macos` should read AI, not AC — short leading words are initials.
    if (words[0].length <= 2) return words[0].toUpperCase();
    if (words.length === 1) return words[0].slice(0, 2).toUpperCase();
    return (words[0][0] + words[1][0]).toUpperCase();
}

export function appMatchesProcess(app: InstalledApp, pid: number): boolean {
    return (app.pids ?? []).includes(pid);
}
