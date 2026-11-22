import { useState } from 'react';

/** The modifier keys the panel can latch. Shift composes with the others. */
export type ModifierName = 'ctrl' | 'alt' | 'meta' | 'shift';

interface ModifierDef {
    name: ModifierName;
    label: string;
}

const MODIFIERS: ModifierDef[] = [
    { name: 'ctrl', label: 'Ctrl' },
    { name: 'alt', label: 'Alt' },
    { name: 'meta', label: '⌘ Cmd' },
    { name: 'shift', label: '⇧ Shift' },
];

/** One-tap combos. The key names match what a page listens for. */
interface ShortcutDef {
    label: string;
    key: string;
    code: string;
    modifiers: ModifierName[];
}

const SHORTCUTS: ShortcutDef[] = [
    { label: 'Ctrl-A', key: 'a', code: 'KeyA', modifiers: ['ctrl'] },
    { label: 'Ctrl-E', key: 'e', code: 'KeyE', modifiers: ['ctrl'] },
    { label: 'Ctrl-C', key: 'c', code: 'KeyC', modifiers: ['ctrl'] },
    { label: 'Ctrl-V', key: 'v', code: 'KeyV', modifiers: ['ctrl'] },
    { label: '⌘-A', key: 'a', code: 'KeyA', modifiers: ['meta'] },
    { label: '⌘-C', key: 'c', code: 'KeyC', modifiers: ['meta'] },
    { label: '⌘-V', key: 'v', code: 'KeyV', modifiers: ['meta'] },
];

export interface BrowserKeysMoreProps {
    /** Modifiers currently latched, in panel order. */
    latched: ModifierName[];
    onToggleModifier: (name: ModifierName) => void;
    /** Press one key with the latched modifiers, then clear them. */
    onKeyWithModifiers: (key: string, code: string, vk: number) => void;
    onShortcut: (key: string, code: string, modifiers: ModifierName[]) => void;
    onCopy: () => void;
    onPaste: (text: string) => void;
    onContextMenu: () => void;
    onCollapse: () => void;
}

/**
 * The keyboard's second page: modifiers, clipboard actions, and the combos that
 * otherwise need a hardware keyboard.
 *
 * Modifiers latch instead of being held: a touch screen cannot hold Ctrl while
 * tapping A, and a long press already means "context menu" in CTRL mode.
 */
export function BrowserKeysMore({
    latched,
    onToggleModifier,
    onKeyWithModifiers,
    onShortcut,
    onCopy,
    onPaste,
    onContextMenu,
    onCollapse,
}: BrowserKeysMoreProps) {
    const [pasteDraft, setPasteDraft] = useState('');
    const latchedSet = new Set(latched);

    // Delete removes the character *after* the caret; Backspace (on the first
    // page) removes the one before it. Both are needed on mobile.
    const pressWithModifiers = (key: string, code: string, vk: number) => {
        onKeyWithModifiers(key, code, vk);
    };

    return (
        <div className="bwp-keysmore" data-keys="more">
            <div className="bwp-keysmore-h">Modifiers · tap to latch</div>
            <div className="bw-keys-row">
                {MODIFIERS.map((mod) => (
                    <button
                        key={mod.name}
                        type="button"
                        className={`bw-key bw-keymod${latchedSet.has(mod.name) ? ' is-on' : ''}`}
                        data-mod={mod.name}
                        aria-pressed={latchedSet.has(mod.name)}
                        onClick={() => onToggleModifier(mod.name)}
                    >
                        {mod.label}
                    </button>
                ))}
                <button
                    type="button"
                    className="bw-key"
                    data-keys="delete"
                    onClick={() => pressWithModifiers('Delete', 'Delete', 46)}
                    title="Delete forward"
                >
                    ⌦ del
                </button>
                <button type="button" className="bw-key bw-key-more" onClick={onCollapse} aria-expanded="true">
                    ▾ Hide
                </button>
            </div>

            <div className="bwp-keysmore-h">Actions</div>
            <div className="bw-keys-row">
                <button type="button" className="bw-key bw-keyact" data-keys="copy" onClick={onCopy}>
                    ⧉ Copy
                </button>
                <button type="button" className="bw-key bw-keyact" data-keys="paste" onClick={() => onPaste(pasteDraft)}>
                    ⎘ Paste
                </button>
                <button type="button" className="bw-key bw-keyact" data-keys="menu" onClick={onContextMenu}>
                    ☰ Menu
                </button>
                <button
                    type="button"
                    className="bw-key bw-keyact"
                    data-keys="select-all"
                    onClick={() => onShortcut('a', 'KeyA', ['ctrl'])}
                >
                    ⤢ Select all
                </button>
            </div>
            <input
                className="bwp-keysmore-paste"
                value={pasteDraft}
                onChange={(event) => setPasteDraft(event.target.value)}
                placeholder="Text to paste into the page…"
                autoCapitalize="none"
                autoCorrect="off"
                spellCheck={false}
                aria-label="Text to paste"
            />

            <div className="bwp-keysmore-h">Shortcuts</div>
            <div className="bw-keys-row">
                {SHORTCUTS.map((cut) => (
                    <button
                        key={cut.label}
                        type="button"
                        className="bw-key"
                        data-shortcut={cut.label}
                        onClick={() => onShortcut(cut.key, cut.code, cut.modifiers)}
                    >
                        {cut.label}
                    </button>
                ))}
            </div>
            <p className="bwp-keysmore-hint">
                {latched.length > 0
                    ? `${latched.join(' + ')} latched: the next key sends the combination.`
                    : 'Latch Ctrl, Alt, or ⌘ then press a key to send a combination.'}
            </p>
        </div>
    );
}
