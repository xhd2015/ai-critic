import { useEffect, useRef } from 'react';
import type { AutoCloseStatus } from './browserWire';
import { autoCloseToast } from './autoClose';

/**
 * Announces windows the daemon closed while the page was open. The first status
 * seen only plants the watermark: records that predate this visit are history,
 * not news, and replaying them on every reload would be noise.
 *
 * The effect depends on the newest record id alone, so the toast fires once per
 * close even when the poll re-renders with an identical payload.
 */
export function useAutoCloseToast(autoClose: AutoCloseStatus | null, notify: (message: string) => void) {
    const seen = useRef<number | null>(null);
    const newest = autoClose?.records?.[0]?.id ?? null;
    const records = autoClose?.records ?? [];

    useEffect(() => {
        if (newest === null) return;
        if (seen.current === null) {
            seen.current = newest;
            return;
        }
        if (newest <= seen.current) return;
        const watermark = seen.current;
        seen.current = newest;
        const message = autoCloseToast(records.filter((record) => record.id > watermark));
        if (message) notify(message);
        // `records` and `notify` are read for their current values only; the
        // newest id is what makes a new close worth announcing.
        // eslint-disable-next-line react-hooks/exhaustive-deps
    }, [newest]);
}
