import { useState } from 'react';
import { formatBytes, formatElapsed, type HostProcess } from '../../../api/processes';
import type { InstalledApp } from '../../../api/apps';
import { LockBadgeIcon } from '../../../pure-view/icons/LockBadgeIcon';
import { PowerIcon } from '../../../pure-view/icons/PowerIcon';
import { appInitials, appStatusText } from './appFilters';
import { processTitle } from './processTree';

/**
 * Installed applications on disk, joined with whatever they are running right
 * now. Running apps expand into their own processes, which reuse the process
 * row markup so kill/open/detail behaviour stays identical.
 */
export function AppList({
    apps,
    loading,
    error,
    onKill,
    onOpen,
}: {
    apps: InstalledApp[];
    loading: boolean;
    error: string | null;
    onKill: (proc: HostProcess) => void;
    onOpen: (proc: HostProcess) => void;
}) {
    const [open, setOpen] = useState<Record<string, boolean>>({});
    const [broken, setBroken] = useState<Record<string, boolean>>({});

    if (error) {
        return <div className="mcc-system-empty">Apps unavailable: {error}</div>;
    }
    if (loading && apps.length === 0) {
        return <div className="mcc-system-empty">Scanning installed apps…</div>;
    }
    if (apps.length === 0) {
        return <div className="mcc-system-empty">No apps match.</div>;
    }

    return (
        <div className="mcc-system-list mcc-app-list">
            {apps.map(app => {
                const running = app.process_count > 0;
                const expanded = open[app.path] ?? (running && app.process_count <= 2);
                const procs = app.processes ?? [];
                const main = procs[0];
                return (
                    <div className={`mcc-system-group mcc-app-group${running ? ' running' : ' idle'}`} key={app.path}>
                        <div
                            className="mcc-app-head"
                            role="button"
                            tabIndex={0}
                            onClick={() => setOpen(cur => ({ ...cur, [app.path]: !expanded }))}
                            onKeyDown={e => {
                                if (e.key === 'Enter' || e.key === ' ') {
                                    e.preventDefault();
                                    setOpen(cur => ({ ...cur, [app.path]: !expanded }));
                                }
                            }}
                        >
                            <span className="mcc-app-chev">{running ? (expanded ? '▾' : '▸') : ''}</span>
                            <span className="mcc-app-icon">
                                {app.has_icon && app.icon_url && !broken[app.path] ? (
                                    <img
                                        src={app.icon_url}
                                        alt=""
                                        loading="lazy"
                                        onError={() => setBroken(cur => ({ ...cur, [app.path]: true }))}
                                    />
                                ) : (
                                    <span className="mcc-app-tile">{appInitials(app.name)}</span>
                                )}
                            </span>
                            <div className="mcc-system-who">
                                <div className="mcc-system-n">
                                    <span className={`mcc-app-name${running ? ' running' : ''}`}>{app.name}</span>
                                    {app.version ? <span className="mcc-app-ver">{app.version}</span> : null}
                                    {app.system ? <span className="mcc-system-tag sys">system</span> : null}
                                </div>
                                <div className="mcc-system-s">
                                    <span className={`mcc-app-procs${running ? ' live' : ''}`}>{appStatusText(app)}</span>
                                </div>
                            </div>
                            <span className={`mcc-system-cpu ${cpuClass(app.cpu_percent)}`}>
                                {running ? `${app.cpu_percent.toFixed(1)}%` : '—'}
                            </span>
                            <span className="mcc-system-mem">{running ? formatBytes(app.rss_bytes) : '—'}</span>
                            {running ? (
                                <button
                                    type="button"
                                    className="mcc-system-kill"
                                    title={`Kill ${app.name} · pid ${main ? main.pid : app.pids?.[0] ?? '?'}`}
                                    onClick={e => {
                                        e.stopPropagation();
                                        if (main) onKill(main);
                                    }}
                                >
                                    <PowerIcon />
                                </button>
                            ) : (
                                <span className="mcc-system-kill dis" title="Not running">
                                    <PowerIcon />
                                </span>
                            )}
                        </div>
                        {expanded &&
                            procs.map(proc => (
                                <AppProcessRow key={proc.pid} proc={proc} onKill={onKill} onOpen={onOpen} />
                            ))}
                    </div>
                );
            })}
        </div>
    );
}

function AppProcessRow({
    proc,
    onKill,
    onOpen,
}: {
    proc: HostProcess;
    onKill: (proc: HostProcess) => void;
    onOpen: (proc: HostProcess) => void;
}) {
    const tags = proc.tags ?? [];
    return (
        <div
            className="mcc-system-row mcc-app-row"
            onClick={() => onOpen(proc)}
            role="button"
            tabIndex={0}
            onKeyDown={e => {
                if (e.key === 'Enter' || e.key === ' ') {
                    e.preventDefault();
                    onOpen(proc);
                }
            }}
        >
            <span />
            <div className="mcc-system-who">
                <div className="mcc-system-n">
                    {processTitle(proc)} · {proc.pid}
                    {tags.includes('stale') ? <span className="mcc-system-tag stale">stale</span> : null}
                    {tags.includes('tun') ? <span className="mcc-system-tag tun">tunnel</span> : null}
                    {proc.protected ? <span className="mcc-system-tag lock">protected</span> : null}
                </div>
                <div className="mcc-system-s">
                    {proc.user} · {formatElapsed(proc.elapsed_sec)} · {proc.cmd}
                </div>
            </div>
            <span className={`mcc-system-cpu ${cpuClass(proc.cpu_percent)}`}>{proc.cpu_percent.toFixed(1)}%</span>
            <span className="mcc-system-mem">{formatBytes(proc.rss_bytes)}</span>
            <button
                type="button"
                className="mcc-system-kill"
                title={proc.protected || 'Kill'}
                onClick={e => {
                    e.stopPropagation();
                    onKill(proc);
                }}
            >
                {proc.protected ? <LockBadgeIcon /> : <PowerIcon />}
            </button>
        </div>
    );
}

function cpuClass(n: number): string {
    if (n >= 80) return 'hot';
    if (n >= 20) return 'warn';
    return '';
}
