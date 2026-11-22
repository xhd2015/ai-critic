import type { AutoCloseRow } from './autoClose';
import { dueLabel, idleShort, nextTen, nextTenCount } from './autoClose';

export interface BrowserNextTenProps {
    rows: AutoCloseRow[];
    thresholdSeconds: number;
    /** The rule is off: show the empty explanation instead of a plan. */
    enabled: boolean;
}

function WindowGlyph() {
    return (
        <svg viewBox="0 0 16 16" fill="none" aria-hidden="true">
            <rect x="5.4" y="2.6" width="8" height="6.8" rx="1.5" stroke="currentColor" strokeWidth="1.2" />
            <rect x="2.6" y="6.6" width="8" height="6.8" rx="1.5" stroke="currentColor" strokeWidth="1.2" />
        </svg>
    );
}

/**
 * The plan the rule is about to execute: windows ranked by last activity, oldest
 * first, capped at ten. It is deliberately a preview, not a second source of
 * truth — the daemon re-checks focus and jobs on every tick, and a window that
 * is used between two polls simply moves down this list.
 */
export function BrowserNextTen({ rows, thresholdSeconds, enabled }: BrowserNextTenProps) {
    if (!enabled) {
        return (
            <div className="bwp-acempty">
                <b>Rule is off</b>
                <span>Turn it on to see which windows would close first.</span>
            </div>
        );
    }
    const total = rows.length;
    const plan = nextTen(rows);
    if (plan.length === 0) {
        return (
            <div className="bwp-acempty">
                <b>No Chrome window is open</b>
                <span>Open a window and it will appear here once it has a clock.</span>
            </div>
        );
    }
    return (
        <>
            <ol className="bwp-nextlist">
                {plan.map((window, index) => {
                    const due = dueLabel(window.idleSeconds, thresholdSeconds);
                    return (
                        <li key={window.key} className="bwp-nextrow">
                            <span className="bwp-nextrank">{index + 1}</span>
                            <span className="bwp-nextico"><WindowGlyph /></span>
                            <span className="bwp-nextmain">
                                <span className="bwp-nexttitle">{window.title}</span>
                                <span className="bwp-nextmeta">
                                    idle {idleShort(window.idleSeconds)} · {window.tabs} {window.tabs === 1 ? 'tab' : 'tabs'}
                                </span>
                            </span>
                            <span className={`bwp-due is-${due.tone}`}>{due.text}</span>
                        </li>
                    );
                })}
            </ol>
            <p className="bwp-nextfoot">{nextTenCount(total, plan.length)}</p>
        </>
    );
}
