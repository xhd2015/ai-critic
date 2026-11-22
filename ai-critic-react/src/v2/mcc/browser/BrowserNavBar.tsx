import type { HistoryStatus } from './browserWire';

export interface BrowserNavBarProps {
    /** Navigation position of the driven tab; arrows disable from this. */
    history?: HistoryStatus;
    /** Disabled while the page is disconnected or read-only. */
    disabled: boolean;
    /** A close or history step is already in flight. */
    busy: boolean;
    onStep: (direction: 'back' | 'forward') => void;
    onCloseTab: () => void;
}

function BackGlyph() {
    return (
        <svg viewBox="0 0 24 24" width="17" height="17" fill="none" stroke="currentColor" strokeWidth="2.1" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true">
            <path d="M15 5 8 12l7 7" />
        </svg>
    );
}

function ForwardGlyph() {
    return (
        <svg viewBox="0 0 24 24" width="17" height="17" fill="none" stroke="currentColor" strokeWidth="2.1" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true">
            <path d="M9 5l7 7-7 7" />
        </svg>
    );
}

function CloseGlyph() {
    return (
        <svg viewBox="0 0 24 24" width="16" height="16" fill="none" stroke="currentColor" strokeWidth="2.1" strokeLinecap="round" aria-hidden="true">
            <path d="M6 6l12 12M18 6L6 18" />
        </svg>
    );
}

/**
 * History arrows and a close button for the driven tab.
 *
 * This is the *browser's* back and forward, not ai-critic's "back to Home":
 * they move through the tab's own navigation entries, which is what a person
 * reaching for the top-left corner expects in a browser chrome.
 */
export function BrowserNavBar({ history, disabled, busy, onStep, onCloseTab }: BrowserNavBarProps) {
    const canBack = !disabled && !busy && !!history?.can_back;
    const canForward = !disabled && !busy && !!history?.can_forward;
    const position = history && history.count > 0 ? `${history.index + 1} / ${history.count}` : '';
    return (
        <div className="bwp-nav" role="group" aria-label="Page history">
            <div className="bwp-navgrp">
                <button
                    type="button"
                    className="bwp-navbtn"
                    data-nav="back"
                    onClick={() => onStep('back')}
                    disabled={!canBack}
                    title={canBack ? 'Back' : 'No earlier page'}
                    aria-label="Back"
                >
                    <BackGlyph />
                </button>
                <button
                    type="button"
                    className="bwp-navbtn"
                    data-nav="forward"
                    onClick={() => onStep('forward')}
                    disabled={!canForward}
                    title={canForward ? 'Forward' : 'No later page'}
                    aria-label="Forward"
                >
                    <ForwardGlyph />
                </button>
                {position && <span className="bwp-navnote">{position}</span>}
            </div>
            <button
                type="button"
                className="bwp-dismiss"
                data-nav="close-tab"
                onClick={onCloseTab}
                disabled={disabled || busy}
                title="Close this tab"
                aria-label="Close this tab"
            >
                <CloseGlyph />
            </button>
        </div>
    );
}
