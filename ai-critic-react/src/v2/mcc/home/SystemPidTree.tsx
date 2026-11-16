import { formatBytes, formatElapsed, type HostProcess } from '../../../api/processes';
import { LockBadgeIcon } from '../../../pure-view/icons/LockBadgeIcon';
import { PowerIcon } from '../../../pure-view/icons/PowerIcon';
import type { VisibleRow } from './groupTree';
import { processTitle } from './processTree';

/**
 * Renders one group's process forest as indented rows.
 *
 * A row is not itself a button: the caret, the body and the kill action are three
 * sibling buttons so keyboard and pointer targets never nest.
 */
export function SystemPidTree({
    rows,
    onToggle,
    onOpen,
    onKill,
}: {
    rows: VisibleRow[];
    onToggle: (pid: number) => void;
    onOpen: (proc: HostProcess) => void;
    onKill: (proc: HostProcess) => void;
}) {
    if (rows.length === 0) {
        return <div className="mcc-system-empty">No processes in this group match the filter.</div>;
    }
    return (
        <div className="mcc-ptree">
            {rows.map(({ node, depth, hasChildren, expanded }) => {
                const proc = node.proc;
                const parent = node.parentOutside;
                const tags = proc.tags ?? [];
                return (
                    <div
                        className={`mcc-ptrow${depth === 0 ? ' root' : ''}`}
                        key={proc.pid}
                        style={{ paddingLeft: 10 + depth * 14 }}
                    >
                        <button
                            type="button"
                            className={`mcc-ptcaret${hasChildren ? '' : ' leaf'}`}
                            disabled={!hasChildren}
                            aria-label={hasChildren ? (expanded ? 'Collapse subtree' : 'Expand subtree') : 'No children in this group'}
                            aria-expanded={hasChildren ? expanded : undefined}
                            onClick={() => onToggle(proc.pid)}
                        >
                            {hasChildren ? (expanded ? '▾' : '▸') : '•'}
                        </button>
                        <button type="button" className="mcc-ptmain" onClick={() => onOpen(proc)}>
                            <span className="mcc-ptname">
                                {processTitle(proc)} · {proc.pid}
                                {depth === 0 ? <span className="mcc-ptroot">root</span> : null}
                                {tags.includes('stale') ? <span className="mcc-system-tag stale">stale</span> : null}
                                {tags.includes('tun') ? <span className="mcc-system-tag tun">tunnel</span> : null}
                                {proc.protected ? <span className="mcc-system-tag lock">protected</span> : null}
                            </span>
                            <span className="mcc-ptmeta">
                                {proc.user} · {formatElapsed(proc.elapsed_sec)} · {proc.state}
                                {depth === 0 ? (parent ? ` · parent ${processTitle(parent)} · ${parent.pid}` : ' · parent outside group') : ''}
                            </span>
                        </button>
                        <span className={`mcc-system-cpu ${cpuClass(proc.cpu_percent)}`}>{proc.cpu_percent.toFixed(1)}%</span>
                        <span className="mcc-system-mem">{formatBytes(proc.rss_bytes)}</span>
                        <button
                            type="button"
                            className="mcc-system-kill"
                            title={proc.protected || `Kill ${processTitle(proc)} · ${proc.pid}`}
                            disabled={!!proc.protected}
                            onClick={() => onKill(proc)}
                        >
                            {proc.protected ? <LockBadgeIcon /> : <PowerIcon />}
                        </button>
                    </div>
                );
            })}
        </div>
    );
}

function cpuClass(n: number): string {
    if (n >= 80) return 'hot';
    if (n >= 20) return 'warn';
    return '';
}
