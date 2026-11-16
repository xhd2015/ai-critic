import { useEffect, useState } from 'react';
import { useNavigate } from 'react-router-dom';
import { BackIcon } from '../../../pure-view/icons/BackIcon';
import { useTabHistory } from '../../../hooks/useTabHistory';
import { NavTabs } from '../types';
import { fetchProcesses, type HostProcess, type ProcessSnapshot } from '../../../api/processes';
import { fetchDisk, type DiskSnapshot } from '../../../api/disk';
import { filterProcesses, orderedPids, type ProcessFilter, type ProcessSort } from './groupProcesses';
import {
    APP_SORTS,
    applyLockedAppOrder,
    filterApps,
    orderedAppPaths,
    sortApps,
    type AppSort,
} from './appFilters';
import { SystemSummary } from './SystemSummary';
import { SystemProcessList } from './SystemProcessList';
import { AppList } from './AppList';
import { SystemKillModal } from './SystemKillModal';
import { useApps } from './useApps';
import './SystemView.css';

/** Either a process filter, or the installed-apps view. */
type ViewMode = ProcessFilter | 'apps';

export function SystemView() {
    const navigate = useNavigate();
    const { goBack } = useTabHistory(NavTabs.Home);
    const [data, setData] = useState<ProcessSnapshot | null>(null);
    const [loading, setLoading] = useState(true);
    const [error, setError] = useState<string | null>(null);
    const [paused, setPaused] = useState(false);
    const [query, setQuery] = useState('');
    const [sort, setSort] = useState<ProcessSort>('cpu');
    const [filter, setFilter] = useState<ViewMode>('all');
    const [appSort, setAppSort] = useState<AppSort>('cpu');
    const [showSystem, setShowSystem] = useState(false);
    const [lockedApps, setLockedApps] = useState<string[] | null>(null);
    const [appsNonce, setAppsNonce] = useState(0);
    const [target, setTarget] = useState<HostProcess | null>(null);
    const [lockedPids, setLockedPids] = useState<number[] | null>(null);
    const [disk, setDisk] = useState<DiskSnapshot | null>(null);
    const [diskError, setDiskError] = useState('');

    useEffect(() => {
        let cancelled = false;
        let timer: number | undefined;
        const tick = async () => {
            try {
                const next = await fetchProcesses();
                if (!cancelled) {
                    setData(next);
                    setError(null);
                }
            } catch (err) {
                if (!cancelled) setError(err instanceof Error ? err.message : String(err));
            } finally {
                if (!cancelled) setLoading(false);
            }
            if (!cancelled && !paused) {
                timer = window.setTimeout(tick, 2000);
            }
        };
        tick();
        return () => {
            cancelled = true;
            if (timer) window.clearTimeout(timer);
        };
    }, [paused]);

    useEffect(() => {
        let cancelled = false;
        let timer: number | undefined;
        const tick = async () => {
            try {
                const next = await fetchDisk();
                if (!cancelled) {
                    setDisk(next);
                    setDiskError('');
                }
            } catch (err) {
                if (!cancelled) setDiskError(err instanceof Error ? err.message : String(err));
            }
            if (!cancelled && !paused) {
                timer = window.setTimeout(tick, 5000);
            }
        };
        tick();
        return () => {
            cancelled = true;
            if (timer) window.clearTimeout(timer);
        };
    }, [paused]);

    const appsActive = filter === 'apps';
    const { view: appsView, error: appsError } = useApps(appsActive, paused, appsNonce);

    const visible = data && !appsActive ? filterProcesses(data.processes, query, filter) : [];
    const allApps = appsView?.apps ?? [];
    const visibleApps = applyLockedAppOrder(
        sortApps(filterApps(allApps, query, showSystem), appSort),
        lockedApps,
    );
    const systemApps = allApps.filter(app => app.system).length;

    return (
        <div className="mcc-system">
            <div className="mcc-section-header" style={{ display: 'flex', alignItems: 'center', gap: 8 }}>
                <button className="mcc-back-btn" onClick={goBack} style={{ background: 'none', border: 'none', cursor: 'pointer', padding: 4 }}>
                    <BackIcon />
                </button>
                <h2 style={{ margin: 0 }}>System</h2>
                <span className={`mcc-system-live${paused ? ' paused' : ''}`}>
                    {paused ? 'paused' : data ? `${Math.max(1, Math.round((data.interval_ms || 2000) / 1000))}s` : '…'}
                </span>
                <button
                    type="button"
                    className={`mcc-system-chip${paused ? ' on' : ''}`}
                    onClick={() => setPaused(v => !v)}
                >
                    {paused ? 'Resume' : 'Pause'}
                </button>
            </div>
            {data && (
                <div className="mcc-system-sub">
                    {data.cpu.num_cpu} cores · {data.counts.processes} processes · host of this page
                </div>
            )}
            {loading && data == null && (
                <div className="mcc-system-empty" style={{ margin: '0 16px' }}>Loading processes…</div>
            )}
            {error && (
                <div className="mcc-system-alert">{error}</div>
            )}
            {data != null && (
                <>
                    <SystemSummary
                        data={data}
                        disk={disk}
                        diskError={diskError}
                        onOpenDisk={() => navigate('disk', { relative: 'path' })}
                    />
                    <div className="mcc-system-controls">
                        <input
                            className="mcc-system-search"
                            type="search"
                            placeholder={
                                appsActive
                                    ? 'Filter by app name, version, bundle id…'
                                    : 'Filter by name, pid, command…'
                            }
                            value={query}
                            onChange={e => setQuery(e.target.value)}
                        />
                        <div className="mcc-system-chips">
                            {appsActive ? (
                                <>
                                    {APP_SORTS.map(([id, label]) => (
                                        <button
                                            key={id}
                                            type="button"
                                            className={`mcc-system-chip${appSort === id && !lockedApps ? ' on' : ''}`}
                                            onClick={() => {
                                                setLockedApps(null);
                                                setAppSort(id);
                                            }}
                                        >
                                            {label}
                                        </button>
                                    ))}
                                    <button
                                        type="button"
                                        className="mcc-system-chip off"
                                        disabled
                                        title="Apps aggregate processes per application, so PID order does not apply"
                                    >
                                        PID
                                    </button>
                                </>
                            ) : (
                                ([
                                    ['cpu', 'CPU ↓'],
                                    ['mem', 'Memory'],
                                    ['name', 'Name'],
                                    ['pid', 'PID'],
                                ] as const).map(([id, label]) => (
                                    <button
                                        key={id}
                                        type="button"
                                        className={`mcc-system-chip${sort === id && !lockedPids ? ' on' : ''}`}
                                        onClick={() => {
                                            setLockedPids(null);
                                            setSort(id);
                                        }}
                                    >
                                        {label}
                                    </button>
                                ))
                            )}
                            <button
                                type="button"
                                className={`mcc-system-chip${(appsActive ? lockedApps : lockedPids) ? ' on' : ''}`}
                                onClick={() => {
                                    if (appsActive) {
                                        setLockedApps(cur => (cur ? null : orderedAppPaths(visibleApps)));
                                        return;
                                    }
                                    if (lockedPids) {
                                        setLockedPids(null);
                                        return;
                                    }
                                    setLockedPids(orderedPids(visible, sort));
                                }}
                            >
                                {(appsActive ? lockedApps : lockedPids) ? 'Unlock order' : 'Lock order'}
                            </button>
                        </div>
                        <div className="mcc-system-chips">
                            {([
                                ['all', 'All'],
                                ['apps', 'Apps'],
                                ['mine', 'Mine'],
                                ['stale', 'Stale / tunnels'],
                            ] as const).map(([id, label]) => (
                                <button
                                    key={id}
                                    type="button"
                                    className={`mcc-system-chip${filter === id ? ' on' : ''}`}
                                    onClick={() => setFilter(id)}
                                >
                                    {label}
                                </button>
                            ))}
                            {appsActive && (
                                <button
                                    type="button"
                                    className={`mcc-system-chip${showSystem ? ' on' : ''}`}
                                    onClick={() => setShowSystem(v => !v)}
                                    title="Bundles under /System/Applications"
                                >
                                    {`System apps${systemApps ? ` · ${systemApps}` : ''}`}
                                </button>
                            )}
                        </div>
                    </div>
                    {(lockedPids || lockedApps) && (
                        <div className="mcc-system-alert" style={{ marginTop: 0 }}>
                            Order locked · numbers still update · lost on refresh
                        </div>
                    )}
                    {appsActive ? (
                        <AppList
                            apps={visibleApps}
                            loading={appsView == null}
                            error={appsError}
                            onKill={setTarget}
                            onOpen={proc => navigate(`pid/${proc.pid}`, { relative: 'path' })}
                        />
                    ) : (
                        <SystemProcessList
                            processes={visible}
                            sort={sort}
                            lockedPids={lockedPids}
                            onKill={setTarget}
                            onOpen={proc => navigate(`pid/${proc.pid}`, { relative: 'path' })}
                            onOpenGroup={name => navigate(`group/${encodeURIComponent(name)}`, { relative: 'path' })}
                        />
                    )}
                </>
            )}
            {target && (
                <SystemKillModal
                    key={target.pid}
                    proc={target}
                    onClose={() => setTarget(null)}
                    onKilled={() => {
                        setTarget(null);
                        fetchProcesses().then(setData).catch(() => {});
                        setAppsNonce(n => n + 1);
                    }}
                />
            )}
        </div>
    );
}
