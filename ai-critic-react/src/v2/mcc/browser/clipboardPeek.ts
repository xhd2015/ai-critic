import type { ClipboardPeek } from './browserWire';

/**
 * Formatting and flavour rules for the Mac-clipboard preview.
 *
 * They live apart from the panel component so they can be unit tested without a
 * DOM: the .tsx file imports them, and the tests import this file directly.
 */

/** "512 B", "91.6 KB", "2.0 MB" */
export function humanBytes(n: number): string {
    if (!Number.isFinite(n) || n <= 0) return '0 B';
    if (n < 1024) return `${n} B`;
    if (n < 1024 * 1024) return `${(n / 1024).toFixed(n < 10 * 1024 ? 1 : 0)} KB`;
    return `${(n / (1024 * 1024)).toFixed(1)} MB`;
}

/** The panel's right-hand size line. */
export function peekSizeLabel(peek: ClipboardPeek | null): string {
    if (!peek || peek.kind === 'empty') return '';
    const bytes = peek.bytes ?? 0;
    if (peek.kind === 'image' && peek.image) {
        const dims = peek.image.width && peek.image.height ? `${peek.image.width}×${peek.image.height} · ` : '';
        return `${dims}${humanBytes(peek.image.bytes || bytes)}`;
    }
    if (peek.kind === 'text' || peek.kind === 'rich') {
        const chars = (peek.text ?? '').length;
        // Once the preview was cut, both numbers matter: how much is shown and
        // how much a copy would take.
        if (peek.truncated) return `${chars} of ${bytes} chars`;
        return `${chars || bytes} chars`;
    }
    if (peek.kind === 'files') return `${peek.files?.length ?? 0} files`;
    return humanBytes(bytes);
}

/** Which flavor the copy button will request, or '' when nothing is copyable. */
export function copyableFlavor(peek: ClipboardPeek | null): string {
    if (!peek || peek.kind === 'empty') return '';
    return peek.copy_flavor ?? '';
}

/** The MIME a flavor must be written as on the phone clipboard. */
export function flavorMIME(flavor: string): string {
    switch (flavor) {
        case 'html':
            return 'text/html';
        case 'png':
            return 'image/png';
        case 'jpeg':
        case 'jpg':
            return 'image/jpeg';
        case 'tiff':
            return 'image/tiff';
        case 'gif':
            return 'image/gif';
        default:
            return 'text/plain';
    }
}

/**
 * The ClipboardItem entries for one flavor.
 *
 * Rich text always carries a plain half in the same item, so a target that only
 * takes text degrades to readable prose instead of pasting nothing.
 */
export function clipboardEntries(flavor: string, peek: ClipboardPeek | null, main: Blob): Record<string, Blob> {
    const entries: Record<string, Blob> = { [flavorMIME(flavor)]: main };
    if (flavor === 'html' && peek?.text) {
        entries['text/plain'] = new Blob([peek.text], { type: 'text/plain' });
    }
    return entries;
}
