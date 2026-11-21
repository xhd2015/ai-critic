import type { AutoCloseStatus, SessionInfo } from './browserWire';
import { idleChipLabel } from './autoClose';

export interface BrowserIdleMetaProps {
    session: SessionInfo;
    autoClose: AutoCloseStatus | null;
    /** The session id line the row normally shows, when the title differed. */
    secondary: string;
}

/**
 * The second line of a rail row. When the rule is armed it gains the window's
 * idle age or its countdown, so a close is visible before it happens.
 */
export function BrowserIdleMeta({ session, autoClose, secondary }: BrowserIdleMetaProps) {
    const idle = autoClose?.enabled ? idleChipLabel(session, autoClose.idle_seconds, true) : '';
    const text = [secondary, idle].filter(Boolean).join(' · ');
    return text ? <span className="bw-sid">{text}</span> : null;
}
