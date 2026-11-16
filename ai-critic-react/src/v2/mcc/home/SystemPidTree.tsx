import { formatBytes, formatElapsed, type HostProcess } from '../../../api/processes';
import { LockBadgeIcon } from '../../../pure-view/icons/LockBadgeIcon';
import { PowerIcon } from '../../../pure-view/icons/PowerIcon';
import { ProcessTreeIcon } from '../../../pure-view/icons/ProcessTreeIcon';
import { cardsFromVisible, type VisibleRow } from './groupTree';
import { processTitle } from './processTree';

/**
 * Group-detail forest, rendered with the same cards as /home/system.
 *
 * Browse: ghead folds, gtree opens the pid page, kill sits left of gtree.
 * Select: depth-0 unprotected roots grow a checkbox; tapping the body toggles
 * the check instead of folding.
 */
export function SystemPidTree({
    rows,
    onToggle,
    onOpen,
    onKill,
    selecting = false,
    selected,
    onToggleSelect,
}: {
    rows: VisibleRow[];
    onToggle: (pid: number) => void;
    onOpen: (proc: HostProcess) => void;
    onKill: (proc: HostProcess) => void;
    selecting?: boolean;
    selected?: ReadonlySet<number>;
    onToggleSelect?: (pid: number) => void;
}) {
    if (rows.length === 0) {
        return <div className="mcc-system-empty">No processes in this group match the filter.</div>;
    }
    return (
        <div className="mcc-system-list">
            {cardsFromVisible(rows).map(card => (
                <div className="mcc-system-group" key={card[0].node.proc.pid}>
                    {card.map(row => (
                        <PidTreeRow
                            key={row.node.proc.pid}
                            row={row}
                            onToggle={onToggle}
                            onOpen={onOpen}
                            onKill={onKill}
                            selecting={selecting}
                            selected={selected}
                            onToggleSelect={onToggleSelect}
                        />
                    ))}
                </div>
            ))}
        </div>
    );
}

function PidTreeRow({
    row,
    onToggle,
    onOpen,
    onKill,
    selecting,
    selected,
    onToggleSelect,
}: {
    row: VisibleRow;
    onToggle: (pid: number) => void;
    onOpen: (proc: HostProcess) => void;
    onKill: (proc: HostProcess) => void;
    selecting: boolean;
    selected?: ReadonlySet<number>;
    onToggleSelect?: (pid: number) => void;
}) {
    const { node, depth, hasChildren, expanded } = row;
    const proc = node.proc;
    const parent = node.parentOutside;
    const tags = proc.tags ?? [];
    const title = processTitle(proc);
    const openLabel = `Open ${title} · ${proc.pid}`;
    const pad = depth > 0 ? { paddingLeft: 10 + depth * 14 } : undefined;
    const selectable = selecting && depth === 0 && !proc.protected;
    const checked = selectable && !!selected?.has(proc.pid);
    const who = (
        <>
            {selectable ? <span className={`mcc-system-check${checked ? ' on' : ''}`}>{checked ? '✓' : ''}</span> : null}
            <span>{hasChildren ? (expanded ? '▾' : '▸') : '•'}</span>
            <div className="mcc-system-who">
                <div className="mcc-system-n">
                    {title} · {proc.pid}
                    {tags.includes('stale') ? <span className="mcc-system-tag stale">stale</span> : null}
                    {tags.includes('tun') ? <span className="mcc-system-tag tun">tunnel</span> : null}
                    {proc.protected ? <span className="mcc-system-tag lock">protected</span> : null}
                </div>
                <div className="mcc-system-s">
                    {proc.user} · {formatElapsed(proc.elapsed_sec)} · {proc.state}
                    {depth === 0 ? (parent ? ` · parent ${processTitle(parent)} · ${parent.pid}` : ' · parent outside group') : ''}
                </div>
            </div>
            <span className={`mcc-system-cpu ${cpuClass(proc.cpu_percent)}`}>{proc.cpu_percent.toFixed(1)}%</span>
            <span className="mcc-system-mem">{formatBytes(proc.rss_bytes)}</span>
        </>
    );
    const headClass = `mcc-system-ghead${selectable ? ' picking' : hasChildren ? '' : ' leaf'}`;
    return (
        <div className={`mcc-system-gwrap with-kill${checked ? ' picked' : ''}`}>
            {selectable || hasChildren ? (
                <button
                    type="button"
                    className={headClass}
                    style={pad}
                    aria-expanded={hasChildren && !selectable ? expanded : undefined}
                    aria-pressed={selectable ? checked : undefined}
                    aria-label={selectable ? (checked ? 'Unselect process' : 'Select process') : (expanded ? 'Collapse subtree' : 'Expand subtree')}
                    onClick={() => {
                        if (selectable) onToggleSelect?.(proc.pid);
                        else onToggle(proc.pid);
                    }}
                >
                    {who}
                </button>
            ) : (
                <div className="mcc-system-ghead leaf" style={pad}>{who}</div>
            )}
            <button
                type="button"
                className="mcc-system-kill"
                title={proc.protected || `Kill ${title} · ${proc.pid}`}
                disabled={!!proc.protected}
                onClick={() => onKill(proc)}
            >
                {proc.protected ? <LockBadgeIcon /> : <PowerIcon />}
            </button>
            <button
                type="button"
                className="mcc-system-gtree"
                title={openLabel}
                aria-label={openLabel}
                onClick={() => onOpen(proc)}
            >
                <ProcessTreeIcon />
            </button>
        </div>
    );
}

function cpuClass(n: number): string {
    if (n >= 80) return 'hot';
    if (n >= 20) return 'warn';
    return '';
}
