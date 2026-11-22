import { useRef, useState } from 'react';
import { fetchClipboardFlavor, peekClipboard } from './browserApi';
import type { ClipboardPeek } from './browserWire';
import { clipboardEntries, copyableFlavor, flavorMIME } from './clipboardPeek';

export type ClipboardCopyState = 'idle' | 'copying' | 'copied' | 'failed';

export interface ClipboardPeekState {
    open: boolean;
    peek: ClipboardPeek | null;
    loading: boolean;
    error: string | null;
    copyState: ClipboardCopyState;
    copyError: string | null;
    toggle: () => void;
    close: () => void;
    refresh: () => void;
    copyToPhone: () => void;
    download: () => void;
}

/**
 * Owns the Mac-clipboard preview and the copy into the phone's clipboard.
 *
 * Bytes are prefetched as soon as the panel has a peek. iOS Safari only allows
 * navigator.clipboard.write() inside a user gesture, so awaiting a fetch after
 * the tap would lose the gesture and the write would be refused; keeping the
 * blob in hand makes the click handler synchronous.
 */
export function useClipboardPeek(): ClipboardPeekState {
    const [open, setOpen] = useState(false);
    const [peek, setPeek] = useState<ClipboardPeek | null>(null);
    const [loading, setLoading] = useState(false);
    const [error, setError] = useState<string | null>(null);
    const [copyState, setCopyState] = useState<ClipboardCopyState>('idle');
    const [copyError, setCopyError] = useState<string | null>(null);
    // Prefetched bytes for the flavor the panel offers, plus the flavor they
    // belong to: a stale blob must never be written under a new flavor.
    const prefetched = useRef<{ flavor: string; blob: Blob } | null>(null);
    const pending = useRef(false);

    const load = () => {
        if (pending.current) return;
        pending.current = true;
        setLoading(true);
        setError(null);
        setCopyState('idle');
        setCopyError(null);
        prefetched.current = null;
        void peekClipboard()
            .then((next) => {
                setPeek(next);
                setLoading(false);
                pending.current = false;
                const flavor = copyableFlavor(next);
                if (!flavor) return;
                // Warm the blob now so the copy click needs no await.
                void fetchClipboardFlavor(flavor)
                    .then((response) => (response.ok ? response.blob() : null))
                    .then((blob) => {
                        if (blob) prefetched.current = { flavor, blob };
                    })
                    .catch(() => {
                        // Prefetch failing is not an error: the click retries.
                    });
            })
            .catch((err: unknown) => {
                setLoading(false);
                setError(err instanceof Error ? err.message : String(err));
                pending.current = false;
            });
    };

    const toggle = () => {
        const next = !open;
        setOpen(next);
        if (next) load();
    };

    const close = () => {
        setOpen(false);
        setCopyState('idle');
        setCopyError(null);
        prefetched.current = null;
    };

    const writeBlob = (flavor: string, blob: Blob) => {
        if (typeof ClipboardItem === 'undefined' || !navigator.clipboard?.write) return false;
        // Rich text also carries a plain half, in the same item, so a plain target
        // degrades to readable text instead of pasting nothing.
        const entries = clipboardEntries(flavor, peek, blob);
        let item: ClipboardItem;
        try {
            item = new ClipboardItem(entries);
        } catch {
            // Some browsers accept only a single type; retry with the primary one.
            try {
                item = new ClipboardItem({ [flavorMIME(flavor)]: blob });
            } catch {
                return false;
            }
        }
        void navigator.clipboard.write([item]).then(
            () => setCopyState('copied'),
            () => fallbackCopy(flavor, blob),
        );
        return true;
    };

    const fallbackCopy = (flavor: string, blob: Blob) => {
        // A plain textarea still works when the async clipboard is refused.
        if (flavor === 'text' || flavor === 'html') {
            void blob.text().then((text) => {
                const area = document.createElement('textarea');
                area.value = text;
                area.setAttribute('readonly', '');
                area.style.position = 'fixed';
                area.style.opacity = '0';
                document.body.appendChild(area);
                area.select();
                const ok = document.execCommand?.('copy') ?? false;
                document.body.removeChild(area);
                setCopyState(ok ? 'copied' : 'failed');
                setCopyError(ok ? null : 'The browser refused to write the clipboard. Long-press the preview to copy instead.');
            });
            return;
        }
        setCopyState('failed');
        setCopyError(
            flavor.startsWith('image/') || ['png', 'jpeg', 'jpg', 'tiff', 'gif'].includes(flavor)
                ? 'This browser will not write images to the clipboard. Use Download, or long-press the preview.'
                : 'The browser refused to write the clipboard.',
        );
    };

    const copyToPhone = () => {
        const flavor = copyableFlavor(peek);
        if (!flavor) return;
        setCopyState('copying');
        setCopyError(null);
        const cached = prefetched.current;
        if (cached && cached.flavor === flavor) {
            if (!writeBlob(flavor, cached.blob)) fallbackCopy(flavor, cached.blob);
            return;
        }
        // No warm blob (prefetch failed or the clipboard changed): fetch and then
        // write. The gesture may be gone, which is exactly when the fallback runs.
        void fetchClipboardFlavor(flavor)
            .then((response) => {
                if (!response.ok) throw new Error(`clipboard read failed (${response.status})`);
                return response.blob();
            })
            .then((blob) => {
                prefetched.current = { flavor, blob };
                if (!writeBlob(flavor, blob)) fallbackCopy(flavor, blob);
            })
            .catch((err: unknown) => {
                setCopyState('failed');
                setCopyError(err instanceof Error ? err.message : String(err));
            });
    };

    const download = () => {
        const flavor = copyableFlavor(peek);
        if (!flavor) return;
        const cached = prefetched.current;
        const start = (blob: Blob) => {
            const url = URL.createObjectURL(blob);
            const a = document.createElement('a');
            a.href = url;
            a.download = peek?.files?.[0]?.name || (flavor === 'png' ? 'clipboard.png' : 'clipboard.txt');
            a.click();
            setTimeout(() => URL.revokeObjectURL(url), 4000);
        };
        if (cached && cached.flavor === flavor) {
            start(cached.blob);
            return;
        }
        void fetchClipboardFlavor(flavor)
            .then((response) => (response.ok ? response.blob() : null))
            .then((blob) => {
                if (blob) start(blob);
            })
            .catch(() => undefined);
    };

    return {
        open,
        peek,
        loading,
        error,
        copyState,
        copyError,
        toggle,
        close,
        refresh: load,
        copyToPhone,
        download,
    };
}
