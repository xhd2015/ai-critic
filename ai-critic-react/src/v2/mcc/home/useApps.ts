import { useEffect, useState } from 'react';
import { fetchApps, type AppsView } from '../../../api/apps';

/**
 * Polls the installed-apps view. Only runs while the Apps chip is active, so
 * the process tab pays nothing for it. The server caches the disk scan for
 * five minutes, so this is cheap even at the 2 s cadence.
 */
export function useApps(
    active: boolean,
    paused: boolean,
    nonce = 0,
): { view: AppsView | null; error: string | null } {
    const [view, setView] = useState<AppsView | null>(null);
    const [error, setError] = useState<string | null>(null);

    useEffect(() => {
        if (!active) return;
        let cancelled = false;
        let timer: number | undefined;
        const tick = async () => {
            try {
                const next = await fetchApps();
                if (!cancelled) {
                    setView(next);
                    setError(null);
                }
            } catch (err) {
                if (!cancelled) setError(err instanceof Error ? err.message : String(err));
            }
            if (!cancelled && !paused) {
                timer = window.setTimeout(tick, 2000);
            }
        };
        tick();
        return () => {
            cancelled = true;
            if (timer) window.clearTimeout(timer);
        };
    }, [active, paused, nonce]);

    return { view, error };
}
