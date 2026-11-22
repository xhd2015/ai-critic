import type { ReactNode } from 'react';
import { BrowserDock } from './BrowserDock';
import { autoCloseRows } from './autoClose';
import { useAutoCloseToast } from './useAutoCloseToast';
import { BrowserKeys } from './BrowserKeys';
import type { ModifierName } from './BrowserKeysMore';
import { BrowserRail } from './BrowserRail';
import { BrowserTabs } from './BrowserTabs';
import { BrowserToolbar } from './BrowserToolbar';
import { BrowserUrlBar } from './BrowserUrlBar';
import type { SweepResult } from './useBrowserSessions';
import type {
    AutoCloseStatus,
    BrowserMode,
    BrowserStatus,
    BrowserTab,
    ConnectionState,
    DaemonStatus,
    Lens,
    SessionInfo,
} from './browserWire';

export interface BrowserDesktopViewProps {
    status: BrowserStatus | null;
    connection: ConnectionState;
    session: string;
    tabs: BrowserTab[];
    activeTabId: number;
    readOnly: boolean;
    windowCloseSupported: boolean;
    autoClose: AutoCloseStatus | null;
    onAutoClose: (enabled: boolean, idleSeconds?: number) => Promise<string | null>;
    helpOpen: boolean;
    sessionless: boolean;
    busy: boolean;
    loading: boolean;
    sessions: SessionInfo[];
    daemon: DaemonStatus | null;
    railOpen: boolean;
    lens: Lens;
    zoom: number;
    keysVisible: boolean;
    fallbackNote: string | null;
    banner: string | null;
    children: ReactNode;
    onHome: () => void;
    onRailToggle: () => void;
    onRailClose: () => void;
    onSelectSession: (session: string) => void;
    onOpenTab: (session: string, tabId: number) => void;
    onCloseRailTab: (session: string, tabId: number) => void;
    onNewWindow: () => void;
    onCloseWindow: (target: { window_id: number } | { session_id: string }) => Promise<string | null>;
    onSweep: (dryRun: boolean) => Promise<SweepResult>;
    onToast: (message: string, warn?: boolean) => void;
    onRefreshSessions: () => void;
    onPreset: (preset: string) => void;
    onMode: (mode: BrowserMode) => void;
    onRefresh: () => void;
    onUrl: (url: string) => void;
    onNewTab: (draft: string) => void;
    onBookmarks: () => void;
    bookmarked: boolean;
    onActivateTab: (tabId: number) => void;
    onCloseTab: (session: string, tabId: number) => void;
    onDismissNote: () => void;
    onDismissBanner: () => void;
    onKeysClose: () => void;
    onText: (text: string) => void;
    onKey: (key: string, code: string, vk?: number) => void;
    onKeyWithModifiers: (key: string, code: string, vk: number, modifiers: ModifierName[]) => void;
    onShortcut: (key: string, code: string, modifiers: ModifierName[]) => void;
    onCopy: () => void;
    onPaste: (text: string) => void;
    onContextMenu: () => void;
    onLens: (lens: Lens) => void;
    onKeys: () => void;
    onHelp: () => void;
}

/** The desktop cockpit: rail, toolbar, tabs, URL bar, keys and dock. */
export function BrowserDesktopView(props: BrowserDesktopViewProps) {
    useAutoCloseToast(props.autoClose, props.onToast);
    return (
        <>
            <BrowserRail
                sessions={props.sessions}
                current={props.session}
                daemon={props.daemon}
                loading={props.loading}
                busy={props.busy}
                readOnly={props.readOnly}
                windowCloseSupported={props.windowCloseSupported}
                autoClose={props.autoClose}
                open={props.railOpen}
                onSelect={props.onSelectSession}
                onOpenTab={props.onOpenTab}
                onCloseTab={props.onCloseRailTab}
                onNew={props.onNewWindow}
                onCloseWindow={props.onCloseWindow}
                onSweep={props.onSweep}
                onToast={props.onToast}
                onRefresh={props.onRefreshSessions}
            />
            {props.railOpen && (
                <button
                    type="button"
                    className="bw-scrim"
                    onClick={props.onRailClose}
                    aria-label="Close sessions"
                />
            )}

            <div className="bw-main">
                <BrowserToolbar
                    status={props.status}
                    connection={props.connection}
                    session={props.session}
                    onBack={props.onHome}
                    onRail={props.onRailToggle}
                    onPreset={props.onPreset}
                    onMode={props.onMode}
                    onRefresh={props.onRefresh}
                />

                {props.readOnly && !props.sessionless && (
                    <div className="bw-ro-banner">
                        browser-agent {props.daemon?.version ?? '?'} cannot switch or close tabs — needs 1.0.20.
                        Watching and clicking still work.
                    </div>
                )}

                <BrowserTabs
                    tabs={props.tabs}
                    activeTabId={props.activeTabId}
                    session={props.session}
                    busy={props.busy}
                    readOnly={props.readOnly || props.sessionless}
                    onActivate={props.onActivateTab}
                    onClose={(tabId) => props.onCloseTab(props.session, tabId)}
                    onCreate={() => props.onNewTab('')}
                />

                <BrowserUrlBar
                    url={props.status?.session.url}
                    disabled={!props.session || props.readOnly || props.sessionless}
                    bookmarked={props.bookmarked}
                    onNavigate={props.onUrl}
                    onNew={props.onNewTab}
                    onBookmarks={props.onBookmarks}
                />
                {props.children}

                {props.fallbackNote && (
                    <button type="button" className="bw-note" onClick={props.onDismissNote}>
                        {props.fallbackNote}
                    </button>
                )}
                {props.banner && (
                    <button type="button" className="bw-toast" onClick={props.onDismissBanner}>
                        {props.banner}
                    </button>
                )}

                <BrowserKeys
                    visible={props.keysVisible}
                    onClose={props.onKeysClose}
                    onText={props.onText}
                    onKey={props.onKey}
                    onKeyWithModifiers={props.onKeyWithModifiers}
                    onShortcut={props.onShortcut}
                    onCopy={props.onCopy}
                    onPaste={props.onPaste}
                    onContextMenu={props.onContextMenu}
                />
                <BrowserDock
                    lens={props.lens}
                    zoom={props.zoom}
                    readOnly={props.readOnly}
                    keysVisible={props.keysVisible}
                    autoClose={props.autoClose}
                    rows={autoCloseRows(props.sessions, props.tabs, props.session)}
                    supported={props.windowCloseSupported}
                    helpOpen={props.helpOpen}
                    onLens={props.onLens}
                    onKeys={props.onKeys}
                    onHelp={props.onHelp}
                    onAutoClose={props.onAutoClose}
                />
            </div>
        </>
    );
}
