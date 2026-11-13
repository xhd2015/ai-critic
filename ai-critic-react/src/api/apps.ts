import type { HostProcess } from './processes';

const API_BASE = '';

/** One installed application, plus its aggregated live process activity. */
export interface InstalledApp {
    name: string;
    version?: string;
    bundle_id?: string;
    path: string;
    system?: boolean;
    exec?: string;
    has_icon?: boolean;
    icon_url?: string;
    process_count: number;
    pids?: number[];
    cpu_percent: number;
    rss_bytes: number;
    processes?: HostProcess[];
}

export interface AppsView {
    scanned_at: string;
    sampled_at?: string;
    platform: string;
    dirs: string[];
    unsupported?: boolean;
    note?: string;
    running: number;
    apps: InstalledApp[];
}

export function emptyAppsView(): AppsView {
    return { scanned_at: '', platform: '', dirs: [], running: 0, apps: [] };
}

export async function fetchApps(refresh = false): Promise<AppsView> {
    const url = `${API_BASE}/api/server/apps${refresh ? '?refresh=1' : ''}`;
    const resp = await fetch(url);
    if (!resp.ok) {
        throw new Error((await resp.text()) || `HTTP ${resp.status}`);
    }
    return (await resp.json()) as AppsView;
}

/** Icon endpoint for an installed app; the browser sends the auth cookie. */
export function appIconUrl(path: string): string {
    return `${API_BASE}/api/server/apps/icon?path=${encodeURIComponent(path)}`;
}
