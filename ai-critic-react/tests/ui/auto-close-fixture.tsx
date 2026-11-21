import { useState } from 'react';
import { createRoot } from 'react-dom/client';
import { BrowserAutoCloseEntry } from '../../src/v2/mcc/browser/BrowserAutoCloseEntry';
import type { AutoCloseStatus, SessionInfo } from '../../src/v2/mcc/browser/browserWire';
import '../../src/v2/mcc/browser/browser.css';

const rows: SessionInfo[] = [
    { id: 's1', current: true, attached: true, attachable: true, idle_seconds: 0, window_id: 900 },
    { id: 's2', current: false, attached: true, attachable: true, idle_seconds: 3480, window_id: 910 },
    { id: 's3', current: false, attached: true, attachable: true, idle_seconds: 600, window_id: 920 },
];

const records = [1, 2, 3].map((id) => ({
    id,
    window_id: 900 + id,
    session_id: `sess-${id}`,
    title: id === 1 ? 'Release notes · Chrome' : id === 2 ? 'PR #482 · ai-critic' : 'Sentry · issues',
    tabs: id + 2,
    idle_seconds: 3600 + id * 120,
    closed_at: new Date(Date.now() - id * 4 * 60_000).toISOString(),
}));

const initial: AutoCloseStatus = { enabled: true, idle_seconds: 3600, supported: true, records };

const surface = new URLSearchParams(window.location.search).get('surface') === 'phone' ? 'phone' : 'rail';

// eslint-disable-next-line react-refresh/only-export-components
function Fixture() {
    const [status, setStatus] = useState<AutoCloseStatus>(initial);
    const [saves, setSaves] = useState(0);
    const onSave = async (enabled: boolean, idleSeconds: number) => {
        await new Promise((resolve) => setTimeout(resolve, 80));
        setStatus((was) => ({ ...was, enabled, idle_seconds: idleSeconds || was.idle_seconds }));
        setSaves((n) => n + 1);
        return null;
    };
    return (
        <div className="bw-page" style={{ display: 'block' }} data-saves={saves} data-surface={surface}>
            <div
                className={surface === 'phone' ? 'bwp-sheet' : ''}
                style={
                    surface === 'phone'
                        ? { position: 'absolute', left: 0, right: 0, bottom: 0, height: '79%', display: 'flex', flexDirection: 'column' }
                        : undefined
                }
            >
                <aside className={surface === 'phone' ? 'bwp-rail is-open' : 'bw-rail'}>
                    <header className={surface === 'phone' ? 'bwp-railhead' : 'bw-rail-head'}>
                        <span className={surface === 'phone' ? 'bwp-kicker' : 'bw-rail-title'}>WINDOWS</span>
                        <span className={surface === 'phone' ? 'bwp-count' : 'bw-rail-count'}>3</span>
                    </header>
                    <div className={surface === 'phone' ? 'bwp-railbody' : 'bw-rail-body'} />
                    <BrowserAutoCloseEntry
                        status={status}
                        rows={rows}
                        busy={false}
                        readOnly={false}
                        supported
                        surface={surface}
                        onSave={onSave}
                    />
                    {surface === 'rail' && <footer className="bw-rail-foot">browser-agent 1.0.30 · :43761</footer>}
                </aside>
            </div>
        </div>
    );
}

createRoot(document.getElementById('root')!).render(<Fixture />);
