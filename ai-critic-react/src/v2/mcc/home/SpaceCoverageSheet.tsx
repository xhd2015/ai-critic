import { useState } from 'react';
import { formatBytes } from '../../../api/processes';
import {
    budgetClock,
    budgetWords,
    coverageText,
    groupDetail,
    rootStateLabel,
    rootStateMark,
    showsFullDiskAccessHint,
    skipKindClass,
    skipKindLabel,
    type CoverageView,
} from './spaceCoverageTone';

interface Props {
    reportId: string;
    /** Bytes the walk managed to read, for the "stopped after" sentence. */
    scannedBytes: number;
    view: CoverageView;
    onClose: () => void;
}

/**
 * The full explanation behind the "not counted" row: how far the walk got, what
 * is missing and why, and what that does and does not affect. Nothing here
 * deletes or retries; it is a report of a report.
 */
export default function SpaceCoverageSheet({ reportId, scannedBytes, view, onClose }: Props) {
    const [copied, setCopied] = useState(false);
    const actionable = view.groups.filter(g => g.kind !== 'by_design');
    const byDesign = view.groups.filter(g => g.kind === 'by_design');
    const unfinished = view.states.filter(s => s.state !== 'done');

    const copy = () => {
        void navigator.clipboard?.writeText(coverageText(view)).then(() => {
            setCopied(true);
            setTimeout(() => setCopied(false), 1500);
        });
    };

    return (
        <div className="mcc-sheet-scrim" onClick={onClose}>
            <div className="mcc-clean-sheet mcc-cov-sheet" onClick={event => event.stopPropagation()}>
                <div className="mcc-sheet-head">
                    <div>
                        <h3>What wasn&apos;t scanned</h3>
                        <div className="mcc-sheet-meta">
                            Report {reportId} · {view.total} folders · {view.groupCount} groups
                            <br />
                            totals below are lower bounds for these paths only
                        </div>
                    </div>
                    <button type="button" className="mcc-sheet-x" onClick={onClose}>
                        ✕
                    </button>
                </div>

                <div className="mcc-sheet-scroll">
                    {view.truncated && (
                        <>
                            <div className="mcc-cov-sect">STOPPED EARLY</div>
                            <div className="mcc-cov-stop">
                                Hit the <b>{budgetWords(view.budgetSeconds)} budget</b> (
                                {view.budgetSeconds ?? 0} s) after {formatBytes(scannedBytes)}.
                                {unfinished.length > 0 ? (
                                    <>
                                        {' '}
                                        These core folders were still in the queue, so their subtrees are{' '}
                                        <b>not counted</b>:
                                    </>
                                ) : (
                                    ' Every core folder finished, so only the folders below are missing.'
                                )}
                                <div className="mcc-cov-roots">
                                    {view.states.map(s => (
                                        <div key={s.root} className="mcc-cov-rootrow">
                                            <span className={`mcc-cov-st ${s.state === 'done' ? 'ok' : 'no'}`}>
                                                {rootStateMark(s.state)}
                                            </span>
                                            <span className="mcc-cov-rootpath">{s.root}</span>
                                            <span className={`mcc-cov-st ${s.state === 'done' ? 'ok' : 'no'}`}>
                                                {rootStateLabel(s.state)}
                                            </span>
                                        </div>
                                    ))}
                                </div>
                            </div>
                        </>
                    )}

                    <div className="mcc-cov-sect">NOT COUNTED · BY REASON</div>
                    {actionable.map(g => (
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
                    {view.other && (
                        <div className="mcc-cov-grow muted">
                            <div className="mcc-cov-gmain">
                                <div className="mcc-cov-gpath">{view.other.groups} more groups</div>
                                <div className="mcc-cov-gex">not listed · counts kept so the total stays exact</div>
                            </div>
                            <div className="mcc-cov-gright">
                                <div className="mcc-cov-gcount">
                                    {view.other.dirs} <small>dirs</small>
                                </div>
                            </div>
                        </div>
                    )}

                    {byDesign.length > 0 && (
                        <>
                            <div className="mcc-cov-sect">SKIPPED ON PURPOSE</div>
                            {byDesign.map(g => (
                                <div key={`${g.kind}:${g.prefix}`} className="mcc-cov-grow">
                                    <div className="mcc-cov-gmain">
                                        <div className="mcc-cov-gpath">{g.prefix}</div>
                                        <div className="mcc-cov-gex">{groupDetail(g)}</div>
                                    </div>
                                    <div className="mcc-cov-gright">
                                        <div className="mcc-cov-gcount">
                                            {g.count} <small>{g.count === 1 ? 'dir' : 'dirs'}</small>
                                        </div>
                                        <span className="mcc-cov-tag k-byconf">by design</span>
                                    </div>
                                </div>
                            ))}
                        </>
                    )}

                    {showsFullDiskAccessHint(view) && (
                        <div className="mcc-cov-hint">
                            <b>{view.permissionDirs} of these need Full Disk Access.</b>
                            <br />
                            Grant it to <code>ai-critic</code> in System Settings › Privacy &amp; Security › Full
                            Disk Access, then re-scan. Sandbox containers and cloud mounts stay excluded either way,
                            and this report already treats them as unknown.
                        </div>
                    )}

                    <div className="mcc-cov-sect">WHAT THIS MEANS</div>
                    <div className="mcc-cov-mean">
                        Nothing was deleted. These folders still exist on disk; their bytes are simply missing from
                        the category breakdown, the biggest-folders list and the trend. The{' '}
                        <b>reclaimable</b> number is measured per cleanup class and is <b>not</b> affected. A
                        complete walk needs {view.roots.length > 0 ? `${view.roots.length} ` : ''}core folders plus
                        the rest of the volume to finish inside the {budgetClock(view.budgetSeconds)} budget.
                    </div>
                </div>

                <div className="mcc-sheet-foot">
                    <button type="button" className="mcc-system-chip" onClick={copy}>
                        {copied ? 'Copied' : 'Copy list'}
                    </button>
                    <button
                        type="button"
                        className="mcc-system-chip"
                        style={{ marginLeft: 'auto' }}
                        onClick={onClose}
                    >
                        Close
                    </button>
                </div>
            </div>
        </div>
    );
}
