import type {
    AnalysisCoverage,
    AnalysisPriorityRootState,
    AnalysisReport,
    AnalysisReportSummary,
    AnalysisSkipGroup,
    AnalysisSkipKind,
    AnalysisSkipOther,
} from '../../../api/diskAnalysis';

type AnyReport = AnalysisReport | AnalysisReportSummary;

/**
 * Everything the coverage UI needs, normalized once. Older reports have no
 * coverage object at all: they still produce a view, with `legacy` set and only
 * a total, because a bare count is all the old scanner recorded.
 */
export interface CoverageView {
    /** Folders missing from every total, of all kinds. Never truncated. */
    total: number;
    groups: AnalysisSkipGroup[];
    other: AnalysisSkipOther | null;
    groupCount: number;
    /** Folders skipped on purpose (cloud drives, autofs mounts). */
    byDesignDirs: number;
    /** Folders that only Full Disk Access would let the walk read. */
    permissionDirs: number;
    roots: string[];
    states: AnalysisPriorityRootState[];
    scanned: number;
    /** The walk hit the soft budget, so the totals are a lower bound. */
    truncated: boolean;
    budgetSeconds: number | null;
    elapsedSeconds: number | null;
    /** No grouped rows: an old report, so offer the count and nothing else. */
    legacy: boolean;
}

type Covered = Pick<AnalysisReport, 'coverage'>;

/**
 * True when every important folder the server knows about finished reading.
 * Reports written before the coverage field existed fall back to `complete`,
 * which is what the old scanner meant by it.
 */
export function priorityComplete(summary: (Covered & { complete?: boolean }) | null | undefined): boolean {
    const coverage: AnalysisCoverage | undefined = summary?.coverage;
    if (!coverage) return Boolean(summary?.complete);
    const roots = coverage.priority_roots?.length ?? 0;
    if (roots === 0) return Boolean(summary?.complete);
    if (typeof coverage.priority_complete === 'boolean') return coverage.priority_complete;
    return coverage.priority_scanned >= roots;
}

/** Rows the card shows before "All groups". */
export const INLINE_SKIP_GROUPS = 4;

/** Below this, the Full Disk Access hint is not worth the space. */
const FDA_HINT_MIN_DIRS = 20;

function totalOf(cov: AnalysisCoverage | undefined, summary: AnyReport): number {
    if (cov && typeof cov.not_counted_total === 'number') return cov.not_counted_total;
    if (typeof summary.not_counted_total === 'number') return summary.not_counted_total;
    return summary.no_access_dirs ?? 0;
}

/**
 * Reads one report's coverage into the shape the views render. Returns null
 * when there is nothing to say: a complete walk with no folders missed.
 */
export function coverageView(summary: AnyReport | null | undefined): CoverageView | null {
    if (!summary) return null;
    const cov = summary.coverage;
    const groups = (cov?.skip_groups ?? []).filter(g => g.count > 0);
    const other = cov?.skip_other && cov.skip_other.dirs > 0 ? cov.skip_other : null;
    const total = totalOf(cov, summary);
    const legacy = !cov || (groups.length === 0 && !other);
    if (!cov && total <= 0) return null;
    if (cov && total <= 0 && groups.length === 0) return null;

    let byDesignDirs = 0;
    let permissionDirs = 0;
    for (const g of groups) {
        if (g.kind === 'by_design') byDesignDirs += g.count;
        if (g.kind === 'permission') permissionDirs += g.count;
    }

    return {
        total,
        groups,
        other,
        groupCount: groups.length + (other?.groups ?? 0),
        byDesignDirs,
        permissionDirs,
        roots: cov?.priority_roots ?? [],
        states: cov?.priority_states ?? [],
        scanned: cov?.priority_scanned ?? 0,
        truncated: Boolean(cov?.truncated ?? summary.truncated),
        budgetSeconds: cov?.budget_s ?? null,
        elapsedSeconds: cov?.elapsed_s ?? null,
        legacy,
    };
}

/** How many core folders finished: "4/6", or null when the server sent none. */
export function priorityFraction(summary: AnyReport | null | undefined): string | null {
    const cov = summary?.coverage;
    const roots = cov?.priority_roots?.length ?? 0;
    if (roots === 0) return null;
    return `${cov?.priority_scanned ?? 0}/${roots}`;
}

/** "permission" / "timed out" — the short tag on a group row. */
export function skipKindLabel(kind: AnalysisSkipKind | undefined): string {
    switch (kind) {
        case 'permission':
            return 'permission';
        case 'timeout':
            return 'timed out';
        case 'io':
            return 'read error';
        case 'by_design':
            return 'by design';
        default:
            return 'skipped';
    }
}

/** Tag colour class, ordered by how much the user can do about it. */
export function skipKindClass(kind: AnalysisSkipKind | undefined): string {
    switch (kind) {
        case 'permission':
            return 'perm';
        case 'timeout':
            return 'time';
        case 'io':
            return 'io';
        case 'by_design':
            return 'byconf';
        default:
            return 'io';
    }
}

/** "10:00" — a budget figure in the same units the scan measured it. */
export function budgetClock(seconds: number | null): string {
    if (!seconds || seconds <= 0) return '—';
    const whole = Math.round(seconds);
    const min = Math.floor(whole / 60);
    const sec = whole % 60;
    return `${min}:${String(sec).padStart(2, '0')}`;
}

/** "10 min" / "90 s" — for prose. */
export function budgetWords(seconds: number | null): string {
    if (!seconds || seconds <= 0) return 'the time budget';
    if (seconds >= 120 && seconds % 60 === 0) return `${seconds / 60} min`;
    return `${Math.round(seconds)} s`;
}

export function rootStateMark(state: string): string {
    if (state === 'done') return '✓';
    if (state === 'partial') return '◔';
    return '○';
}

export function rootStateLabel(state: string): string {
    if (state === 'done') return 'done';
    if (state === 'partial') return 'partial';
    return 'not started';
}

/** The groups the card shows inline; the rest are behind "All groups". */
export function inlineGroups(view: CoverageView): AnalysisSkipGroup[] {
    return view.groups.slice(0, INLINE_SKIP_GROUPS);
}

/** "4 more groups (49 dirs) · 2 by design" — the footer under the inline list. */
export function inlineRest(view: CoverageView): string {
    const restGroups = view.groups.length - inlineGroups(view).length + (view.other?.groups ?? 0);
    const restDirs =
        view.groups.slice(INLINE_SKIP_GROUPS).reduce((sum, g) => sum + g.count, 0) + (view.other?.dirs ?? 0);
    if (restGroups <= 0 && restDirs <= 0) return '';
    const byDesign = view.byDesignDirs > 0 ? ` · ${view.byDesignDirs} by design` : '';
    if (restGroups <= 0) return `${restDirs} more dirs${byDesign}`;
    return `${restGroups} more ${restGroups === 1 ? 'group' : 'groups'} (${restDirs} dirs)${byDesign}`;
}

/** Shown only when enough folders would actually become readable. */
export function showsFullDiskAccessHint(view: CoverageView): boolean {
    return view.permissionDirs >= FDA_HINT_MIN_DIRS;
}

/** One line: "operation timed out after 1s · e.g. …/com.apple.mail/Data". */
export function groupDetail(group: AnalysisSkipGroup): string {
    const parts: string[] = [];
    if (group.reason) parts.push(group.reason);
    if (group.example) parts.push(`e.g. ${group.example}`);
    return parts.join(' · ');
}

/** The plain-text list the sheet's "Copy list" button puts on the clipboard. */
export function coverageText(view: CoverageView): string {
    const lines = [
        `${view.total} folders not counted, ${view.groupCount} groups` +
            (view.truncated ? ` · walk hit the ${budgetWords(view.budgetSeconds)} budget` : ''),
    ];
    for (const g of view.groups) {
        lines.push(`${g.count}\t${g.prefix}\t${skipKindLabel(g.kind)}${g.reason ? `\t${g.reason}` : ''}`);
    }
    if (view.other) lines.push(`${view.other.dirs}\t(${view.other.groups} more groups, not listed)`);
    return lines.join('\n');
}

/**
 * " · partial 4/6 · 10 min budget" for a history or trend row. Empty when the
 * core folders finished, so callers can append it unconditionally.
 */
export function partialNote(summary: AnyReport | null | undefined): string {
    const cov = summary?.coverage;
    const complete = priorityComplete(summary);
    if (complete) return '';
    const fraction = priorityFraction(summary);
    const budget = cov?.truncated ? ` · ${budgetWords(cov?.budget_s ?? null)} budget` : '';
    return ` · partial${fraction ? ` ${fraction}` : ''}${budget}`;
}
