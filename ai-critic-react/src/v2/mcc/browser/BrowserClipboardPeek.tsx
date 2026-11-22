import { useState } from 'react';
import type { ClipboardPeek } from './browserWire';
import { copyableFlavor, humanBytes, peekSizeLabel } from './clipboardPeek';

export interface BrowserClipboardPeekProps {
    peek: ClipboardPeek | null;
    loading: boolean;
    error: string | null;
    /** Outcome of the last copy, shown as a one-line confirmation. */
    copyState: 'idle' | 'copying' | 'copied' | 'failed';
    copyError: string | null;
    onRefresh: () => void;
    onCopyToPhone: () => void;
    /** Insert this clipboard into the page instead of the phone clipboard. */
    onPasteIntoPage: () => void;
    onDownload: () => void;
    onClose: () => void;
}

function kindLabel(peek: ClipboardPeek | null): string {
    switch (peek?.kind) {
        case 'text':
            return 'text';
        case 'rich':
            return 'rich text';
        case 'image':
            return 'image';
        case 'files':
            return 'files';
        case 'empty':
            return 'empty';
        default:
            return 'clipboard';
    }
}

/**
 * The Mac clipboard, shown beside the keyboard.
 *
 * Nothing here polls: the panel reads the clipboard when it opens and when the
 * person asks again, because a clipboard often holds a password and a passive
 * reader would ship it to the phone unprompted.
 */
export function BrowserClipboardPeek({
    peek,
    loading,
    error,
    copyState,
    copyError,
    onRefresh,
    onCopyToPhone,
    onPasteIntoPage,
    onDownload,
    onClose,
}: BrowserClipboardPeekProps) {
    const [expanded, setExpanded] = useState(false);
    const empty = !peek || peek.kind === 'empty';
    const flavor = copyableFlavor(peek);
    const canCopy = !empty && !!flavor && copyState !== 'copying';

    return (
        <div className="bwp-peek" data-peek="panel">
            <div className="bwp-peek-h">
                <span className="bwp-peek-k">Mac clipboard</span>
                <span className="bwp-peek-kind" data-peek="kind">{kindLabel(peek)}</span>
                <span className="bwp-peek-sz" data-peek="size">{peekSizeLabel(peek)}</span>
                <button type="button" className="bwp-peek-x" onClick={onClose} aria-label="Close clipboard preview">
                    ✕
                </button>
            </div>

            {loading && <div className="bwp-peek-body"><p className="bwp-peek-dim">Reading the Mac clipboard…</p></div>}

            {!loading && error && (
                <div className="bwp-peek-body">
                    <p className="bwp-peek-err" role="alert">{error}</p>
                </div>
            )}

            {!loading && !error && empty && (
                <div className="bwp-peek-body">
                    <p className="bwp-peek-dim">{peek?.note || 'The Mac clipboard is empty.'}</p>
                </div>
            )}

            {!loading && !error && !empty && peek && (
                <>
                    {!!peek.flavors?.length && (
                        <div className="bwp-peek-flavors" data-peek="flavors">
                            {peek.flavors.slice(0, 6).map((name) => (
                                <span
                                    key={name}
                                    className={`bwp-peek-fl${name === flavor || (flavor === 'text' && name === 'utf8') ? ' is-on' : ''}`}
                                >
                                    {name}
                                </span>
                            ))}
                        </div>
                    )}

                    <div className="bwp-peek-body">
                        {peek.kind === 'rich' && peek.html ? (
                            // The HTML comes from another app; a sandboxed iframe keeps
                            // its scripts and styles out of this page.
                            <iframe
                                className={`bwp-peek-rich${expanded ? ' is-tall' : ''}`}
                                title="Clipboard preview"
                                sandbox=""
                                srcDoc={peek.html}
                                data-peek="html"
                            />
                        ) : peek.kind === 'image' && peek.image ? (
                            <div className="bwp-peek-img" data-peek="image">
                                <span>{peek.image.mime} · {peekSizeLabel(peek)}</span>
                            </div>
                        ) : peek.kind === 'files' && peek.files ? (
                            <ul className="bwp-peek-files" data-peek="files">
                                {peek.files.slice(0, expanded ? 20 : 3).map((file) => (
                                    <li key={file.path}>
                                        <i aria-hidden="true" />
                                        <span className="bwp-peek-name">{file.name}</span>
                                        {file.size ? <span className="bwp-peek-fsz">{humanBytes(file.size)}</span> : null}
                                    </li>
                                ))}
                            </ul>
                        ) : (
                            <button
                                type="button"
                                className={`bwp-peek-text${expanded ? ' is-tall' : ''}`}
                                onClick={() => setExpanded((was) => !was)}
                                data-peek="text"
                                aria-expanded={expanded}
                                title={expanded ? 'Collapse' : 'Show all'}
                            >
                                {peek.text || '(no text)'}
                            </button>
                        )}

                        {peek.truncated && (
                            <p className="bwp-peek-dim">
                                Preview cut to {peek.text?.length ?? 0} chars; the copy takes the whole value.
                            </p>
                        )}
                        {peek.note && <p className="bwp-peek-dim">{peek.note}</p>}
                    </div>

                    <div className="bwp-peek-f">
                        <button
                            type="button"
                            className="bwp-peek-btn is-primary"
                            onClick={onCopyToPhone}
                            disabled={!canCopy}
                            data-peek="copy"
                        >
                            {copyState === 'copying' ? 'Copying…' : copyState === 'copied' ? '✓ Copied' : '⧉ Copy to iPhone'}
                        </button>
                        <button
                            type="button"
                            className="bwp-peek-btn"
                            onClick={onPasteIntoPage}
                            disabled={!peek.text && !peek.html}
                            data-peek="paste"
                        >
                            ⎘ Paste into page
                        </button>
                        <button type="button" className="bwp-peek-btn is-ghost" onClick={onRefresh} aria-label="Re-read clipboard">
                            ↻
                        </button>
                    </div>

                    {copyState === 'failed' && copyError && (
                        <div className="bwp-peek-f">
                            <p className="bwp-peek-err" role="alert">{copyError}</p>
                        </div>
                    )}

                    {(peek.kind === 'image' || peek.kind === 'files') && (
                        <div className="bwp-peek-f">
                            <button type="button" className="bwp-peek-btn" onClick={onDownload} data-peek="download">
                                ⤓ Download
                            </button>
                        </div>
                    )}
                </>
            )}
        </div>
    );
}
