import { useEffect, useMemo, useRef, useState } from 'react';
import { useNavigate, useParams } from 'react-router-dom';
import { BackIcon } from '../../../pure-view/icons/BackIcon';
import { fetchProcesses, formatBytes, formatElapsed, type HostProcess, type ProcessSnapshot } from '../../../api/processes';
import {
    allCollapsed,
    buildGroupForest,
    defaultCollapsed,
    filterForest,
    flattenVisible,
} from './groupTree';
import { SystemPidTree } from './SystemPidTree';
import { SystemKillModal } from './SystemKillModal';
import './SystemView.css';

/** Above this many members the tree opens only the first two levels. */
const AUTO_COLLAPSE_THRESHOLD = 40;

const EMPTY_COLLAPSED: Set<number> = new Set();

export function SystemGroupPage() {
    const { name: nameParam } = useParams();
    const groupName = nameParam ?? '';
    const navigate = useNavigate();
    const [data, setData] = useState<ProcessSnapshot | null>(null);
    const [loading, setLoading] = useState(true);
    const [error, setError] = useState<string | null>(null);
    const [paused, setPaused] = useState(false);
    const [query, setQuery] = useState('');
    const [collapsed, setCollapsed] = useState<Set<number> | null>(null);
    const [killTarget, setKillTarget] = useState<HostProcess | null>(null);
    const seeded = useRef(false);

    useEffect(() => {
        let cancelled = false;
        let timer: number | undefined;
        const tick = async () => {
            try {
                const next = await fetchProcesses();
                if (!cancelled) {
                    setData(next);
                    setError(null);
                    // First snapshot decides the initial collapse depth; afterwards the user owns it.
                    if (!seeded.current) {
                        const tree = buildGroupForest(next.processes, groupName);
                        if (tree.count > 0) {
                            seeded.current = true;
                            setCollapsed(tree.count > AUTO_COLLAPSE_THRESHOLD ? defaultCollapsed(tree.roots, 2) : EMPTY_COLLAPSED);
                        }
                    }
                }
            } catch (err) {
                if (!cancelled) setError(err instanceof Error ? err.message : String(err));
            } finally {
                if (!cancelled) setLoading(false);
            }
            if (!cancelled && !paused) timer = window.setTimeout(tick, 2000);
        };
        tick();
        return () => {
            cancelled = true;
            if (timer) window.clearTimeout(timer);
        };
    }, [paused, groupName]);

    const forest = useMemo(
        () => buildGroupForest(data?.processes ?? [], groupName),
        [data, groupName],
    );

    const effectiveCollapsed = collapsed ?? EMPTY_COLLAPSED;
    const rows = flattenVisible(filterForest(forest.roots, query), effectiveCollapsed);

    const toggle = (pid: number) => {
        setCollapsed(cur => {
            const next = new Set(cur ?? []);
            if (next.has(pid)) next.delete(pid);
            else next.add(pid);
            return next;
        });
    };

    return (
        <div className="mcc-system">
            <div className="mcc-section-header" style={{ display: 'flex', alignItems: 'center', gap: 8 }}>
                <button
                    className="mcc-back-btn"
                    onClick={() => navigate('../..', { relative: 'path' })}
                    style={{ background: 'none', border: 'none', cursor: 'pointer', padding: 4 }}
                >
                    <BackIcon />
                </button>
                <h2 style={{ margin: 0, minWidth: 0, overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}>
                    {groupName || 'group'}
                </h2>
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
            <div className="mcc-system-sub">System / group {groupName}</div>
            {loading && data == null && (
                <div className="mcc-system-empty" style={{ margin: '0 16px' }}>Loading group…</div>
            )}
            {error && <div className="mcc-system-alert">{error}</div>}
            {data != null && forest.count === 0 && (
                <div className="mcc-system-empty" style={{ margin: '0 16px' }}>
                    Group “{groupName}” is no longer running.
                </div>
            )}
            {data != null && forest.count > 0 && (
                <>
                    <div className="mcc-system-tiles">
                        <div className="mcc-system-tile">
                            <div className="mcc-system-tile-lab">Group CPU</div>
                            <div className={`mcc-system-tile-val ${cpuClass(forest.cpu)}`}>{forest.cpu.toFixed(1)}%</div>
                            <div className="mcc-system-tile-meta">Σ {forest.count} procs</div>
                        </div>
                        <div className="mcc-system-tile">
                            <div className="mcc-system-tile-lab">Group RSS</div>
                            <div className="mcc-system-tile-val">{formatBytes(forest.rss)}</div>
                            <div className="mcc-system-tile-meta">Σ {forest.count} procs</div>
                        </div>
                        <div className="mcc-system-tile">
                            <div className="mcc-system-tile-lab">Processes</div>
                            <div className="mcc-system-tile-val">{forest.count}</div>
                            <div className="mcc-system-tile-meta">
                                {forest.rootCount} root{forest.rootCount === 1 ? '' : 's'} in group
                            </div>
                        </div>
                        <div className="mcc-system-tile">
                            <div className="mcc-system-tile-lab">Oldest</div>
                            <div className="mcc-system-tile-val">{formatElapsed(forest.oldestSec)}</div>
                            <div className="mcc-system-tile-meta">uptime, e.g. 2d1h</div>
                        </div>
                    </div>
                    <div className="mcc-ptip">
                        Kills are per-process: killing a parent leaves its children running. Tap a parent to fold its subtree; use the tree button to open the pid page.
                    </div>
                    <div className="mcc-ptools">
                        <input
                            className="mcc-system-search"
                            type="search"
                            placeholder={`Filter ${forest.count} procs by name, pid, command…`}
                            value={query}
                            onChange={e => setQuery(e.target.value)}
                        />
                        <div className="mcc-system-chips">
                            <button
                                type="button"
                                className="mcc-system-chip"
                                onClick={() => setCollapsed(new Set())}
                            >
                                Expand all
                            </button>
                            <button
                                type="button"
                                className="mcc-system-chip"
                                onClick={() => setCollapsed(allCollapsed(forest.roots))}
                            >
                                Collapse all
                            </button>
                        </div>
                    </div>
                    <SystemPidTree
                        rows={rows}
                        onToggle={toggle}
                        onOpen={proc => navigate(`../../pid/${proc.pid}`, { relative: 'path' })}
                        onKill={setKillTarget}
                    />
                </>
            )}
            {killTarget && (
                <SystemKillModal
                    key={killTarget.pid}
                    proc={killTarget}
                    onClose={() => setKillTarget(null)}
                    onKilled={() => {
                        setKillTarget(null);
                        fetchProcesses().then(setData).catch(() => {});
                    }}
                />
            )}
        </div>
    );
}

function cpuClass(n: number): string {
    if (n >= 80) return 'hot';
    if (n >= 20) return 'warn';
    return '';
}
