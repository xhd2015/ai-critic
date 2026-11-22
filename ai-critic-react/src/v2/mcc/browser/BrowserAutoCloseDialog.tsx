import { useEffect, useState } from 'react';
import type { AutoCloseStatus } from './browserWire';
import type { AutoCloseRow } from './autoClose';
import { BrowserNextTen } from './BrowserNextTen';
import {
    AUTO_CLOSE_MAX_SECONDS,
    AUTO_CLOSE_MIN_SECONDS,
    AUTO_CLOSE_PRESETS,
    AUTO_CLOSE_RECORD_LIMIT,
    customSeconds,
    idleLabel,
    idleShort,
    presetOf,
    recordAgo,
    recordSummary,
    secondsOfPreset,
} from './autoClose';

export interface BrowserAutoCloseDialogProps {
    status: AutoCloseStatus | null;
    /** Live windows for the Next 10 preview; the daemon re-checks on each tick. */
    rows: AutoCloseRow[];
    /** 'rail' is the desktop modal; 'phone' is the bottom sheet. */
    surface: 'rail' | 'phone';
    readOnly: boolean;
    supported: boolean;
    onClose: () => void;
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

function initialDraft(seconds: number): { preset: number | 'custom'; value: string; unit: 'minutes' | 'hours' } {
    const preset = presetOf(seconds);
    if (preset !== 'custom') return { preset, value: '30', unit: 'minutes' };
    if (seconds % 3600 === 0) return { preset: 'custom', value: String(seconds / 3600), unit: 'hours' };
    return { preset: 'custom', value: String(Math.round(seconds / 60)), unit: 'minutes' };
}

/**
 * The one rule editor, shared by the desktop rail and the phone sheet. It owns
 * the draft so Cancel really cancels: the switch and the segmented control move
 * a local value, and only Save reaches the daemon.
 */
export function BrowserAutoCloseDialog(props: BrowserAutoCloseDialogProps) {
    const { status, rows, surface, readOnly, supported, onClose } = props;
    const initial = initialDraft(status?.idle_seconds ?? 3600);
    const [enabled, setEnabled] = useState(Boolean(status?.enabled));
    const [preset, setPreset] = useState<number | 'custom'>(initial.preset);
    const [customValue, setCustomValue] = useState(initial.value);
    const [unit, setUnit] = useState<'minutes' | 'hours'>(initial.unit);
    const [saving, setSaving] = useState(false);
    const [error, setError] = useState('');

    useEffect(() => {
        const onKey = (event: KeyboardEvent) => {
            if (event.key !== 'Escape' || saving) return;
            event.preventDefault();
            event.stopPropagation();
            onClose();
        };
        document.addEventListener('keydown', onKey, true);
        return () => document.removeEventListener('keydown', onKey, true);
    }, [onClose, saving]);

    const threshold =
        preset === 'custom' ? customSeconds(Number(customValue), unit) : secondsOfPreset(preset);
    const records = status?.records ?? [];
    const blocked = !supported || readOnly;

    const save = async () => {
        if (saving) return;
        setSaving(true);
        setError('');
        const failure = await props.onSave(enabled, threshold);
        setSaving(false);
        if (failure) setError(failure);
        else onClose();
    };

    // Switching to Custom starts from the threshold already in effect, so the
    // number field shows the value the person is about to edit, not a surprise.
    const chooseCustom = () => {
        if (preset === 'custom') return;
        const draft = initialDraft(threshold);
        setPreset('custom');
        setCustomValue(draft.value);
        setUnit(draft.unit);
    };

    return (
        <div
            className={`bwp-aclayer is-${surface}`}
            onClick={(event) => {
                if (event.target === event.currentTarget && !saving) onClose();
            }}
        >
            <div className="bwp-acdlg" role="dialog" aria-modal="true" aria-labelledby="bwp-actitle">
                <header className="bwp-achead">
                    <span className="kicker">AUTO-CLOSE</span>
                    <h3 id="bwp-actitle">Idle windows</h3>
                    <p>Close Chrome windows nobody has used for a while. No exceptions: the window you are viewing and the last window close too.</p>
                    <button type="button" className="bwp-acx" onClick={onClose} disabled={saving} aria-label="Close">
                        ×
                    </button>
                </header>

                <div className="bwp-acbody">
                    <div className="bwp-actorow">
                        <div className="bwp-accopy">
                            <div className="bwp-aclabel">Auto-close idle windows</div>
                            <div className="bwp-achint">Runs in the daemon, so it keeps working while this page is closed.</div>
                        </div>
                        <button
                            type="button"
                            role="switch"
                            className={`bwp-switch${enabled ? ' is-on' : ''}`}
                            aria-checked={enabled}
                            aria-label="Auto-close idle windows"
                            disabled={blocked || saving}
                            onClick={() => setEnabled((was) => !was)}
                        >
                            <span />
                        </button>
                    </div>

                    <section className="bwp-acsec">
                        <div className="bwp-acsechead">
                            <span className="kicker">CLOSE AFTER</span>
                            <span className="bwp-acsecval">{idleLabel(threshold)}</span>
                        </div>
                        <div className="bwp-acseg" role="group" aria-label="Close idle windows after">
                            {AUTO_CLOSE_PRESETS.map((minutes) => (
                                <button
                                    key={minutes}
                                    type="button"
                                    className={preset === minutes ? 'on' : ''}
                                    onClick={() => setPreset(minutes)}
                                    disabled={saving}
                                >
                                    {idleShort(minutes * 60)}
                                </button>
                            ))}
                            <button
                                type="button"
                                className={preset === 'custom' ? 'on' : ''}
                                onClick={chooseCustom}
                                disabled={saving}
                            >
                                Custom
                            </button>
                        </div>
                        {preset === 'custom' && (
                            <div className="bwp-accustom">
                                <input
                                    type="number"
                                    min={unit === 'hours' ? 1 : AUTO_CLOSE_MIN_SECONDS / 60}
                                    max={unit === 'hours' ? 24 : AUTO_CLOSE_MAX_SECONDS / 60}
                                    step={unit === 'hours' ? 1 : 5}
                                    value={customValue}
                                    onChange={(event) => setCustomValue(event.target.value)}
                                    aria-label="Idle minutes"
                                    disabled={saving}
                                />
                                <select
                                    value={unit}
                                    onChange={(event) => setUnit(event.target.value === 'hours' ? 'hours' : 'minutes')}
                                    aria-label="Idle unit"
                                    disabled={saving}
                                >
                                    <option value="minutes">minutes</option>
                                    <option value="hours">hours</option>
                                </select>
                                <span className="bwp-achint">5 min – 24 h</span>
                            </div>
                        )}
                        <p className="bwp-acpreview">
                            Windows left untouched for <b>{idleLabel(threshold)}</b> close automatically. Any click,
                            keystroke or agent job resets the clock.
                        </p>
                    </section>

                    <section className="bwp-acsec">
                        <div className="bwp-acsechead">
                            <span className="kicker">NEXT 10</span>
                            <span className="bwp-acsecnote">oldest activity first</span>
                        </div>
                        <BrowserNextTen rows={rows} thresholdSeconds={threshold} enabled={enabled} />
                        <p className="bwp-acskip">
                            No exemptions. Closing the last window quits Chrome; the next start reopens one.
                        </p>
                    </section>

                    <section className="bwp-aclog">
                        <div className="bwp-acsechead">
                            <span className="kicker">CLOSE RECORD</span>
                            <span className="bwp-account">{records.length} of {AUTO_CLOSE_RECORD_LIMIT} · in memory</span>
                        </div>
                        {records.length === 0 ? (
                            <div className="bwp-acempty">
                                <b>No windows closed yet</b>
                                <span>Windows that hit the threshold show up here, newest first.</span>
                            </div>
                        ) : (
                            <ol className="bwp-acreclist">
                                {records.map((record) => (
                                    <li key={record.id} className="bwp-acrecl">
                                        <span className="bwp-acrecico"><ClockGlyph /></span>
                                        <div className="bwp-acrecbody">
                                            <div className="bwp-acrection">
                                                {record.title?.trim() || record.session_id || `Window ${record.window_id}`}
                                            </div>
                                            <div className="bwp-acrecm">{recordSummary(record)}</div>
                                        </div>
                                        <span className="bwp-acrectime">{recordAgo(record.closed_at)}</span>
                                    </li>
                                ))}
                            </ol>
                        )}
                        <p className="bwp-acnote">
                            Records live in daemon memory; restarting the daemon clears them.
                        </p>
                    </section>

                    {error && (
                        <p className="bwp-acerr" role="alert">
                            {error}
                        </p>
                    )}
                </div>

                <div className="bwp-acfoot">
                    <span className="bwp-acfootnote">
                        {supported ? `Daemon · ${idleShort(threshold)} · keeps ${AUTO_CLOSE_RECORD_LIMIT}` : 'Window close needs browser-agent 1.0.22+'}
                    </span>
                    <button type="button" className="bwp-acghost" onClick={onClose} disabled={saving}>
                        Cancel
                    </button>
                    <button type="button" className="bwp-acsave" onClick={() => void save()} disabled={saving || blocked} aria-busy={saving}>
                        {saving ? 'Saving…' : 'Save'}
                    </button>
                </div>
            </div>
        </div>
    );
}
