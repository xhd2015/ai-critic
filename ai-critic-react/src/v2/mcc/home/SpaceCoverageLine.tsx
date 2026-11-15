import { useState } from 'react';
import {
    budgetClock,
    groupDetail,
    inlineGroups,
    inlineRest,
    skipKindClass,
    skipKindLabel,
    type CoverageView,
} from './spaceCoverageTone';

interface Props {
    view: CoverageView;
    /** Opens the full sheet; the inline panel is a preview of it. */
    onOpen: () => void;
}

/**
 * The "not counted" row under a report: one muted line, and on tap the first
 * few folded groups. The line stays collapsed by default because a complete
 * scan has nothing there, and a partial one should not push the categories off
 * the screen.
 */
export default function SpaceCoverageLine({ view, onOpen }: Props) {
    const [open, setOpen] = useState(false);
    const shown = inlineGroups(view);
    const rest = inlineRest(view);
    const canExpand = shown.length > 0;
    const why = view.truncated
        ? `The walk hit the ${budgetClock(view.budgetSeconds)} budget, so these folders were only partly read.`
        : 'These folders could not be read, so their bytes are missing from the totals.';

    return (
        <div className="mcc-cov">
            <button
                type="button"
                className={`mcc-cov-line${open ? ' open' : ''}`}
                onClick={() => (canExpand ? setOpen(v => !v) : onOpen())}
                aria-expanded={canExpand ? open : undefined}
            >
                <span className="mcc-cov-dot" />
                <span className="mcc-cov-t">{view.total} folders not counted</span>
                <span className="mcc-cov-sub">
                    {view.groupCount > 0 ? ` · ${view.groupCount} groups` : ''}
                    {view.truncated ? ` · stopped at ${budgetClock(view.budgetSeconds)}` : ''}
                </span>
                {canExpand && <span className="mcc-cov-more">{open ? 'Hide ⌃' : 'Details ⌄'}</span>}
            </button>

            {open && canExpand && (
                <div className="mcc-cov-panel">
                    <div className="mcc-cov-why">{why}</div>
                    {shown.map(g => (
                        <div key={`${g.kind}:${g.prefix}`} className="mcc-cov-grow">
                            <div className="mcc-cov-gmain">
                                <div className="mcc-cov-gpath">{g.prefix}</div>
                                <div className="mcc-cov-gex">{groupDetail(g)}</div>
                            </div>
                            <div className="mcc-cov-gright">
                                <div className="mcc-cov-gcount">
                                    {g.count} <small>{g.count === 1 ? 'dir' : 'dirs'}</small>
                                </div>
                                <span className={`mcc-cov-tag k-${skipKindClass(g.kind)}`}>
                                    {skipKindLabel(g.kind)}
                                </span>
                            </div>
                        </div>
                    ))}
                    <div className="mcc-cov-foot">
                        <div className="mcc-cov-note">{rest || 'Nothing was deleted; the numbers are lower bounds.'}</div>
                        <button type="button" className="mcc-cov-all" onClick={onOpen}>
                            All groups ›
                        </button>
                    </div>
                </div>
            )}
        </div>
    );
}
