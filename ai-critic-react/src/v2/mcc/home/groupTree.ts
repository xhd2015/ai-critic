import type { HostProcess } from '../../../api/processes';

/**
 * One node of a group's process forest.
 *
 * Topology comes from `ppid` only — never from `name`. Chrome helpers all run as
 * `Google Chrome` but show up as `Renderer` / `Network` / `GPU` via
 * `processTitle()`, so grouping by name would flatten the tree.
 */
export interface GroupNode {
    proc: HostProcess;
    depth: number;
    children: GroupNode[];
    isRoot: boolean;
    /** Parent lives outside the group (launchd, another app). Only set on roots. */
    parentOutside?: HostProcess;
}

export interface GroupForest {
    name: string;
    members: HostProcess[];
    roots: GroupNode[];
    count: number;
    cpu: number;
    rss: number;
    /** Largest `elapsed_sec` in the group — the "group uptime". */
    oldestSec: number;
    /** Members that are PID roots inside the group. */
    rootCount: number;
}

export function groupMembers(list: HostProcess[], name: string): HostProcess[] {
    return list.filter(p => p.name === name);
}

export function buildGroupForest(list: HostProcess[], name: string): GroupForest {
    const members = groupMembers(list, name);
    const byPid = new Map<number, HostProcess>();
    for (const p of members) byPid.set(p.pid, p);
    const nodes = new Map<number, GroupNode>();
    for (const p of members) {
        nodes.set(p.pid, { proc: p, depth: 0, children: [], isRoot: false });
    }
    const roots: GroupNode[] = [];
    const outside = new Map<number, HostProcess | undefined>();
    for (const p of members) {
        const node = nodes.get(p.pid)!;
        const parent = p.ppid !== p.pid ? byPid.get(p.ppid) : undefined;
        if (parent) {
            nodes.get(parent.pid)!.children.push(node);
        } else {
            node.isRoot = true;
            if (!outside.has(p.ppid)) outside.set(p.ppid, list.find(c => c.pid === p.ppid));
            const parentProc = outside.get(p.ppid);
            if (parentProc) node.parentOutside = parentProc;
            roots.push(node);
        }
    }
    if (roots.length === 0 && members.length > 0) {
        // Defensive: every member claims a parent inside the group (a ppid cycle, which
        // the kernel does not produce). Promote one node so the page is never empty.
        const first = [...members].sort((a, b) => a.pid - b.pid)[0];
        const parent = nodes.get(first.ppid);
        if (parent) parent.children = parent.children.filter(c => c.proc.pid !== first.pid);
        const node = nodes.get(first.pid)!;
        node.isRoot = true;
        delete node.parentOutside;
        roots.push(node);
    }
    sortNodes(roots);
    const seen = new Set<number>();
    const attach = (node: GroupNode, depth: number) => {
        if (seen.has(node.proc.pid)) {
            // A ppid cycle cannot be rendered; drop the back-edge instead of recursing forever.
            node.children = [];
            node.isRoot = false;
            return;
        }
        seen.add(node.proc.pid);
        node.depth = depth;
        sortNodes(node.children);
        for (const child of node.children) attach(child, depth + 1);
    };
    for (const root of roots) attach(root, 0);
    const cpu = members.reduce((s, p) => s + (p.cpu_percent || 0), 0);
    const rss = members.reduce((s, p) => s + (p.rss_bytes || 0), 0);
    const oldestSec = members.reduce((s, p) => Math.max(s, p.elapsed_sec || 0), 0);
    return { name, members, roots, count: members.length, cpu, rss, oldestSec, rootCount: roots.length };
}

function sortNodes(nodes: GroupNode[]) {
    nodes.sort((a, b) => (b.proc.cpu_percent || 0) - (a.proc.cpu_percent || 0) || a.proc.pid - b.proc.pid);
}

export interface VisibleRow {
    node: GroupNode;
    depth: number;
    hasChildren: boolean;
    expanded: boolean;
}

/** Depth-first rows, skipping everything under a collapsed pid. */
export function flattenVisible(roots: GroupNode[], collapsed: ReadonlySet<number>): VisibleRow[] {
    const out: VisibleRow[] = [];
    const walk = (node: GroupNode, depth: number) => {
        const hasChildren = node.children.length > 0;
        const expanded = hasChildren && !collapsed.has(node.proc.pid);
        out.push({ node, depth, hasChildren, expanded });
        if (expanded) for (const child of node.children) walk(child, depth + 1);
    };
    for (const root of roots) walk(root, 0);
    return out;
}

/** Split a flattened walk into one card per depth-0 root, with its visible descendants. */
export function cardsFromVisible(rows: VisibleRow[]): VisibleRow[][] {
    const cards: VisibleRow[][] = [];
    for (const row of rows) {
        if (row.depth === 0 || cards.length === 0) cards.push([row]);
        else cards[cards.length - 1].push(row);
    }
    return cards;
}

/** Every pid in the subtree, including the node itself. */
export function subtreePids(node: GroupNode): number[] {
    const out: number[] = [];
    const walk = (n: GroupNode) => {
        out.push(n.proc.pid);
        for (const child of n.children) walk(child);
    };
    walk(node);
    return out;
}

export function matchesNode(proc: HostProcess, query: string): boolean {
    const q = query.trim().toLowerCase();
    if (!q) return true;
    return (
        (proc.name || '').toLowerCase().includes(q) ||
        (proc.cmd || '').toLowerCase().includes(q) ||
        (proc.user || '').toLowerCase().includes(q) ||
        String(proc.pid).includes(q)
    );
}

/**
 * Keep nodes that match, plus the ancestors needed to reach them. When a node
 * itself matches, its whole subtree is kept so the topology below it stays readable.
 */
export function filterForest(roots: GroupNode[], query: string): GroupNode[] {
    const q = query.trim();
    if (!q) return roots;
    const walk = (node: GroupNode): GroupNode | null => {
        if (matchesNode(node.proc, q)) return node;
        const kept: GroupNode[] = [];
        for (const child of node.children) {
            const next = walk(child);
            if (next) kept.push(next);
        }
        if (kept.length === 0) return null;
        return { ...node, children: kept };
    };
    const out: GroupNode[] = [];
    for (const root of roots) {
        const next = walk(root);
        if (next) out.push(next);
    }
    reindexDepths(out);
    return out;
}

function reindexDepths(roots: GroupNode[]) {
    const walk = (node: GroupNode, depth: number) => {
        node.depth = depth;
        for (const child of node.children) walk(child, depth + 1);
    };
    for (const root of roots) walk(root, 0);
}

/** Collapse state for a forest: open the first `limit` levels, collapse everything below. */
export function defaultCollapsed(roots: GroupNode[], limit: number): Set<number> {
    const out = new Set<number>();
    const walk = (node: GroupNode, depth: number) => {
        if (depth >= limit && node.children.length > 0) {
            out.add(node.proc.pid);
            return;
        }
        for (const child of node.children) walk(child, depth + 1);
    };
    for (const root of roots) walk(root, 0);
    return out;
}

/** Ids of every node that currently has children — what "Collapse all" needs. */
export function branchPids(roots: GroupNode[]): number[] {
    const out: number[] = [];
    const walk = (node: GroupNode) => {
        if (node.children.length > 0) out.push(node.proc.pid);
        for (const child of node.children) walk(child);
    };
    for (const root of roots) walk(root);
    return out;
}

export function allCollapsed(roots: GroupNode[]): Set<number> {
    return new Set(branchPids(roots));
}
