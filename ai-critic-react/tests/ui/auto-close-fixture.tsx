import { useState } from 'react';
import { createRoot } from 'react-dom/client';
import { BrowserMoreMenu } from '../../src/v2/mcc/browser/BrowserMoreMenu';
import type { AutoCloseStatus } from '../../src/v2/mcc/browser/browserWire';
import type { AutoCloseRow } from '../../src/v2/mcc/browser/autoClose';
import '../../src/v2/mcc/browser/browser.css';

// Twelve windows, so the Next 10 cap is visible in the fixture and in the test.
const rows: AutoCloseRow[] = [
    { key: 'sess-current', title: 'about:blank', tabCount: 2, current: true, idle_seconds: 180 },
    { key: 'sess-a', title: 'Docs — ai-critic', tabCount: 4, idle_seconds: 3480 },
    { key: 'sess-b', title: 'Release notes · Chrome', tabCount: 3, idle_seconds: 3660 },
    { key: 'sess-c', title: 'PR #482 · ai-critic', tabCount: 6, idle_seconds: 4320 },
    { key: 'sess-d', title: 'Sentry · issues', tabCount: 2, idle_seconds: 3615 },
    { key: 'sess-e', title: 'Linear · ENG-482', tabCount: 3, idle_seconds: 7200 },
    { key: 'sess-f', title: 'Inbox — Gmail', tabCount: 8, idle_seconds: 2400 },
    { key: 'sess-g', title: 'Figma · Browser v3', tabCount: 5, idle_seconds: 5400 },
    { key: 'sess-h', title: 'Grafana · api-latency', tabCount: 4, idle_seconds: 1800 },
    { key: 'sess-i', title: 'Notion · browser spec', tabCount: 3, idle_seconds: 900 },
    { key: 'sess-j', title: 'YouTube', tabCount: 2, idle_seconds: 60 },
    { key: 'sess-k', title: 'Calendar', tabCount: 1, idle_seconds: 1200 },
];

const records = [1, 2, 3].map((id) => ({
    id,
    window_id: 900 + id,
    session_id: `sess-old-${id}`,
    title: id === 1 ? 'Release notes · Chrome' : id === 2 ? 'PR #482 · ai-critic' : 'Sentry · issues',
    tabs: id + 2,
    idle_seconds: 3600 + id * 120,
    closed_at: new Date(Date.now() - id * 4 * 60_000).toISOString(),
}));

const initial: AutoCloseStatus = { enabled: true, idle_seconds: 3600, supported: true, records };

// eslint-disable-next-line react-refresh/only-export-components
function Fixture() {
    const [status, setStatus] = useState<AutoCloseStatus>(initial);
    const [saves, setSaves] = useState(0);
    const [helpOpen, setHelpOpen] = useState(false);
    const onSave = async (enabled: boolean, idleSeconds: number) => {
        await new Promise((resolve) => setTimeout(resolve, 60));
        setStatus((was) => ({ ...was, enabled, idle_seconds: idleSeconds || was.idle_seconds }));
        setSaves((n) => n + 1);
        return null;
    };
    return (
        <div className="bw-page" style={{ display: 'block' }} data-saves={saves}>
            <div className="bwp-sheet-shell" style={{ position: 'relative', height: 820, width: 402, overflow: 'visible' }}>
                <div className="bwp-shead">
                    <span className="bwp-kicker">Browser</span>
                    <span className="bwp-live">12 windows</span>
                    <span className="sp" style={{ flex: 1 }} />
                    <div className="bwp-headacts">
                        <BrowserMoreMenu
                            surface="sheet"
                            status={status}
                            rows={rows}
                            readOnly={false}
                            supported
                            helpOpen={helpOpen}
                            onHelp={() => setHelpOpen(true)}
                            onSave={onSave}
                        />
                    </div>
                </div>
                <div className="bwp-railbody" />
                {helpOpen && <p data-help-open>help</p>}
            </div>
        </div>
    );
}

createRoot(document.getElementById('root')!).render(<Fixture />);
