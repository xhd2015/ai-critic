import { useState } from 'react';
import type { AutoCloseStatus } from './browserWire';
import { autoCloseBlocker, idleShort, nextCloseLabel } from './autoClose';
import type { AutoCloseRow } from './autoClose';
import { BrowserAutoCloseDialog } from './BrowserAutoCloseDialog';

export interface BrowserAutoCloseEntryProps {
    status: AutoCloseStatus | null;
    rows: AutoCloseRow[];
    busy: boolean;
    readOnly: boolean;
    supported: boolean;
    /** 'rail' is the desktop rail footer, 'phone' the sheet's rail footer. */
    surface: 'rail' | 'phone';
    /** Resolves to an error sentence, or null when the rule was saved. */
    onSave: (enabled: boolean, idleSeconds: number) => Promise<string | null>;
}

function ClockGlyph() {
    return (
        <svg viewBox="0 0 16 16" fill="none" aria-hidden="true">
            <circle cx="8" cy="8" r="5.9" stroke="currentColor" strokeWidth="1.3" />
            <path d="M8 4.7V8l2.1 1.4" stroke="currentColor" strokeWidth="1.3" strokeLinecap="round" />
        </svg>
    );
}

/**
 * The rail's rule entry: one line of state and one switch, with the full editor
 * behind the label. The switch is a shortcut, not a substitute: a person can
 * arm the timer without opening the record, and the record stays one click away.
 */
export function BrowserAutoCloseEntry(props: BrowserAutoCloseEntryProps) {
    const { status, rows, busy, readOnly, supported, surface, onSave } = props;
    const [open, setOpen] = useState(false);
    const [saving, setSaving] = useState(false);
    const [error, setError] = useState('');

    const enabled = Boolean(status?.enabled);
    const threshold = status?.idle_seconds ?? 3600;
    const blocker = autoCloseBlocker({ supported, readOnly, busy, status });
    const countdown = nextCloseLabel(rows, threshold, enabled);

    const toggle = async () => {
        if (saving || !status || readOnly || !supported) return;
        setSaving(true);
        setError('');
        const failure = await onSave(!enabled, 0);
        setSaving(false);
        if (failure) setError(failure);
    };

    return (
        <>
            <div className={`bwp-auto is-${surface}${enabled ? '' : ' is-off'}`}>
                <div className="bwp-actorow">
                    <button
                        type="button"
                        className="bwp-automain"
                        onClick={() => setOpen(true)}
                        title={blocker || 'Auto-close settings and record'}
                        aria-label={`Auto-close idle windows: ${enabled ? idleShort(threshold) : 'off'}`}
                    >
                        <span className="bwp-autoico"><ClockGlyph /></span>
                        <span className="bwp-autocopy">
                            <span className="bwp-autotitle">
                                Auto-close idle
                                <span className={`bwp-autodot${enabled ? ' is-on' : ''}`} />
                            </span>
                            <span className="bwp-autosub">
                                {enabled ? `${idleShort(threshold)} · ${countdown}` : 'Off · windows stay open'}
                            </span>
                        </span>
                    </button>
                    <button
                        type="button"
                        role="switch"
                        className={`bwp-switch${enabled ? ' on' : ''}`}
                        aria-checked={enabled}
                        aria-label="Auto-close idle windows"
                        disabled={Boolean(blocker) || saving}
                        onClick={() => void toggle()}
                    >
                        <i />
                    </button>
                </div>
                {error && <p className="bwp-autoerr" role="alert">{error}</p>}
            </div>
            {open && (
                <BrowserAutoCloseDialog
                    status={status}
                    surface={surface}
                    readOnly={readOnly}
                    supported={supported}
                    onClose={() => setOpen(false)}
                    onSave={onSave}
                />
            )}
        </>
    );
}
