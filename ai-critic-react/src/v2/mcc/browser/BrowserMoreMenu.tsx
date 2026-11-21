import { useEffect, useRef, useState } from 'react';
import type { AutoCloseStatus } from './browserWire';
import type { AutoCloseRow } from './autoClose';
import { idleShort, nextCloseLabel } from './autoClose';
import { BrowserAutoCloseDialog } from './BrowserAutoCloseDialog';

export interface BrowserMoreMenuProps {
    /** 'sheet' is the phone header icon, 'dock' the desktop bottom bar. */
    surface: 'sheet' | 'dock';
    status: AutoCloseStatus | null;
    rows: AutoCloseRow[];
    readOnly: boolean;
    supported: boolean;
    helpOpen: boolean;
    onHelp: () => void;
    /** Saves the rule; resolves to an error sentence, or null. */
    onSave: (enabled: boolean, idleSeconds?: number) => Promise<string | null>;
}

function DotGlyph() {
    return (
        <svg viewBox="0 0 20 20" fill="none" aria-hidden="true">
            <circle cx="4.6" cy="10" r="1.5" fill="currentColor" />
            <circle cx="10" cy="10" r="1.5" fill="currentColor" />
            <circle cx="15.4" cy="10" r="1.5" fill="currentColor" />
        </svg>
    );
}

function ClockGlyph() {
    return (
        <svg viewBox="0 0 16 16" fill="none" aria-hidden="true">
            <circle cx="8" cy="8" r="5.9" stroke="currentColor" strokeWidth="1.3" />
            <path d="M8 4.7V8l2.1 1.4" stroke="currentColor" strokeWidth="1.3" strokeLinecap="round" />
        </svg>
    );
}

function HelpGlyph() {
    return (
        <svg viewBox="0 0 16 16" fill="none" aria-hidden="true">
            <circle cx="8" cy="8" r="6" stroke="currentColor" strokeWidth="1.3" />
            <path d="M6.3 6.2a1.8 1.8 0 1 1 2.4 1.7c-.5.2-.7.6-.7 1.1v.3" stroke="currentColor" strokeWidth="1.3" strokeLinecap="round" />
            <circle cx="8" cy="11.4" r=".8" fill="currentColor" />
        </svg>
    );
}

interface Anchor {
    top: number;
    left: number;
}

/**
 * The header/dock overflow. It replaces the lone `?` with one home for Help and
 * the auto-close rule; the rule row is a single button whose pill is the state,
 * and clicking it opens the configuration sheet.
 *
 * The popover is fixed to the viewport and anchored to the trigger's rect: the
 * phone sheet and the desktop dock both clip their children, so a plain absolute
 * popover would disappear at the edges.
 */
export function BrowserMoreMenu(props: BrowserMoreMenuProps) {
    const { surface, status, rows, readOnly, supported, helpOpen, onHelp, onSave } = props;
    const [open, setOpen] = useState(false);
    const [cfgOpen, setCfgOpen] = useState(false);
    const [anchor, setAnchor] = useState<Anchor>({ top: 0, left: 0 });
    const triggerRef = useRef<HTMLButtonElement>(null);
    const menuRef = useRef<HTMLDivElement>(null);

    const enabled = Boolean(status?.enabled);
    const threshold = status?.idle_seconds ?? 3600;
    const sub = !supported
        ? 'Needs browser-agent 1.0.22+'
        : enabled
          ? `${idleShort(threshold)} · ${nextCloseLabel(rows, threshold, true)}`
          : 'Nothing closes on its own';
    const pill = !supported ? '—' : enabled ? `On · ${idleShort(threshold)}` : 'Off';

    const place = () => {
        const trigger = triggerRef.current;
        if (!trigger) return;
        const rect = trigger.getBoundingClientRect();
        const width = 274;
        const left = Math.max(8, Math.min(window.innerWidth - width - 8, rect.right - width));
        const top = surface === 'dock' ? Math.max(8, rect.top - 184) : rect.bottom + 8;
        setAnchor({ top, left });
    };

    const show = () => {
        place();
        setOpen(true);
    };

    const hide = (returnFocus = true) => {
        setOpen(false);
        if (returnFocus) triggerRef.current?.focus({ preventScroll: true });
    };

    useEffect(() => {
        if (!open) return;
        const onDown = (event: MouseEvent) => {
            const target = event.target as Node | null;
            if (!target) return;
            if (triggerRef.current?.contains(target) || menuRef.current?.contains(target)) return;
            hide(false);
        };
        const onKey = (event: KeyboardEvent) => {
            if (event.key !== 'Escape') return;
            event.preventDefault();
            event.stopPropagation();
            hide();
        };
        const onResize = () => place();
        document.addEventListener('mousedown', onDown);
        document.addEventListener('keydown', onKey, true);
        window.addEventListener('resize', onResize);
        return () => {
            document.removeEventListener('mousedown', onDown);
            document.removeEventListener('keydown', onKey, true);
            window.removeEventListener('resize', onResize);
        };
    });

    return (
        <>
            <button
                ref={triggerRef}
                type="button"
                className={`${surface === 'sheet' ? 'bwp-hbtn' : 'bw-btn bw-btn-ghost'}${open ? ' is-on' : ''}`}
                onClick={() => (open ? hide() : show())}
                aria-haspopup="menu"
                aria-expanded={open}
                aria-label="More"
                title="Help and auto-close"
            >
                <span aria-hidden="true"><DotGlyph /></span>
                {enabled && <span className="bwp-moredot" aria-hidden="true" />}
            </button>

            {open && (
                <div
                    ref={menuRef}
                    className={`bwp-moremenu is-${surface}`}
                    style={{ top: anchor.top, left: anchor.left }}
                    role="menu"
                    aria-label="More"
                >
                    <div className="bwp-morecap">More</div>
                    <button
                        type="button"
                        className={`bwp-morerow${helpOpen ? ' is-on' : ''}`}
                        data-more="help"
                        role="menuitem"
                        onClick={() => { hide(); onHelp(); }}
                    >
                        <span className="bwp-moreico"><HelpGlyph /></span>
                        <span className="bwp-morecopy">
                            <span className="bwp-moretitle">Help</span>
                            <span className="bwp-moresub">Gestures &amp; keys</span>
                        </span>
                    </button>
                    <button
                        type="button"
                        className="bwp-morerow"
                        data-more="auto-close"
                        role="menuitem"
                        onClick={() => { hide(false); setCfgOpen(true); }}
                    >
                        <span className="bwp-moreico is-blue"><ClockGlyph /></span>
                        <span className="bwp-morecopy">
                            <span className="bwp-moretitle">Auto-close idle</span>
                            <span className="bwp-moresub">{sub}</span>
                        </span>
                        <span className={`bwp-statepill${enabled ? ' is-on' : ''}${!supported ? ' is-na' : ''}`}>{pill}</span>
                    </button>
                    <div className="bwp-morenote">One button: the pill is the state, the row opens the rule.</div>
                </div>
            )}

            {cfgOpen && (
                <BrowserAutoCloseDialog
                    status={status}
                    rows={rows}
                    surface={surface === 'sheet' ? 'phone' : 'rail'}
                    readOnly={readOnly}
                    supported={supported}
                    onClose={() => setCfgOpen(false)}
                    onSave={onSave}
                />
            )}
        </>
    );
}
