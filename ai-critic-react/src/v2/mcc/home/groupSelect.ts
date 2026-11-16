import type { VisibleRow } from './groupTree';

/** Depth-0 roots that may be checked in Select mode (not protected). */
export function selectableRootPids(rows: VisibleRow[]): number[] {
    const out: number[] = [];
    for (const row of rows) {
        if (row.depth !== 0) continue;
        if (row.node.proc.protected) continue;
        out.push(row.node.proc.pid);
    }
    return out;
}

export function pruneSelected(selected: ReadonlySet<number>, selectable: readonly number[]): Set<number> {
    const allowed = new Set(selectable);
    const next = new Set<number>();
    for (const pid of selected) {
        if (allowed.has(pid)) next.add(pid);
    }
    return next;
}
