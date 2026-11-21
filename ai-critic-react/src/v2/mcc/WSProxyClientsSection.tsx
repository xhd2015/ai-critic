import { useEffect, useState } from 'react';
import {
    createWSClient,
    deleteWSClient,
    fetchWSClients,
    probeWSClient,
    startWSClient,
    stopWSClient,
    updateWSClient,
    wsClientPACURL,
    type WSClientRecord,
    type WSClientStatus,
} from '../../api/proxyServices';
import { WSProxyLogArea } from './WSProxyLogArea';
import type { LogLine } from './wsProxyStream';

const POLL_MS = 3000;

function formatUptime(secs: number | undefined): string {
    if (!secs || secs < 0) return '';
    if (secs < 60) return `${secs}s`;
    if (secs < 3600) return `${Math.floor(secs / 60)}:${String(secs % 60).padStart(2, '0')}`;
    const h = Math.floor(secs / 3600);
    const m = Math.floor((secs % 3600) / 60);
    return `${h}:${String(m).padStart(2, '0')}:${String(secs % 60).padStart(2, '0')}`;
}

/**
 * WSProxyClientsSection is the Client half of the WS Proxy page: the local xray
 * tunnels this machine consumes. Each row shows the inbound ports, the source
 * instance, and the live state, and can start, stop, probe, edit or delete.
 */
export function WSProxyClientsSection() {
    const [clients, setClients] = useState<WSClientRecord[]>([]);
    const [statuses, setStatuses] = useState<WSClientStatus[]>([]);
    const [error, setError] = useState<string | null>(null);
    const [showAdd, setShowAdd] = useState(false);
    const [pendingId, setPendingId] = useState<string | null>(null);
    const [logFor, setLogFor] = useState<string | null>(null);
    const [logLines, setLogLines] = useState<LogLine[]>([]);
    const [probeResult, setProbeResult] = useState<{ id: string; ok: boolean; text: string } | null>(null);
    const [editing, setEditing] = useState<string | null>(null);
    const [confirmDeleteId, setConfirmDeleteId] = useState<string | null>(null);

    const refresh = async () => {
        try {
            const { clients: list, statuses: states } = await fetchWSClients();
            setClients(list);
            setStatuses(states);
            setError(null);
        } catch (err) {
            setError(err instanceof Error ? err.message : String(err));
        }
    };

    useEffect(() => {
        void refresh();
        const timer = window.setInterval(() => { void refresh(); }, POLL_MS);
        return () => window.clearInterval(timer);
    }, []);

    const append = (text: string, isError?: boolean) => {
        setLogLines((prev) => [...prev, { text, error: isError }]);
    };

    const runStart = async (c: WSClientStatus) => {
        setPendingId(c.id);
        setLogFor(c.id);
        setLogLines([]);
        append(`client ${c.id}: starting…`);
        try {
            const st = await startWSClient(c.id);
            append(`xray up · http 127.0.0.1:${st.http_port} · socks 127.0.0.1:${st.socks_port}`);
            append('egress probe ok');
        } catch (err) {
            append(err instanceof Error ? err.message : String(err), true);
        } finally {
            setPendingId(null);
            await refresh();
        }
    };

    const runStop = async (c: WSClientStatus) => {
        setPendingId(c.id);
        setLogFor(c.id);
        setLogLines([]);
        append(`client ${c.id}: stopping…`);
        try {
            await stopWSClient(c.id);
            append('stopped');
        } catch (err) {
            append(err instanceof Error ? err.message : String(err), true);
        } finally {
            setPendingId(null);
            await refresh();
        }
    };

    const runProbe = async (c: WSClientStatus) => {
        setPendingId(c.id);
        try {
            const res = await probeWSClient(c.id);
            setProbeResult({
                id: c.id,
                ok: res.ok,
                text: res.ok ? `egress ok: ${res.url}` : res.error ?? 'probe failed',
            });
        } catch (err) {
            setProbeResult({ id: c.id, ok: false, text: err instanceof Error ? err.message : String(err) });
        } finally {
            setPendingId(null);
        }
    };

    const copyPAC = async (c: WSClientStatus) => {
        try {
            const resp = await fetch(wsClientPACURL(c.id));
            if (!resp.ok) throw new Error(`PAC fetch failed: ${resp.status}`);
            await navigator.clipboard.writeText(await resp.text());
            setProbeResult({ id: c.id, ok: true, text: 'PAC copied to clipboard' });
        } catch (err) {
            setProbeResult({ id: c.id, ok: false, text: err instanceof Error ? err.message : String(err) });
        }
    };

    const remove = async (c: WSClientStatus) => {
        setPendingId(c.id);
        try {
            await deleteWSClient(c.id);
            setConfirmDeleteId(null);
            setLogFor(null);
            await refresh();
        } catch (err) {
            setError(err instanceof Error ? err.message : String(err));
        } finally {
            setPendingId(null);
        }
    };

    const running = statuses.filter((s) => s.running).length;

    return (
        <>
            <div className="mcc-headerbar">
                <span className={`mcc-pill ${running > 0 ? 'run' : 'stop'}`}>{running}/{statuses.length} running</span>
                <span className="mcc-spacer" />
                <button className="mcc-btn mcc-btn-primary" onClick={() => setShowAdd((v) => !v)}>+ Add Client</button>
            </div>
            <div className="mcc-sub">
                本地 xray 消费远端实例：HTTP/SOCKS 入站 → VMess/WS → 远端 ai-critic 出口。
                运行在服务端进程里，重启机器后随 ai-critic 自动恢复。
            </div>

            {error && <div className="mcc-svc-detail-error">{error}</div>}

            {showAdd && (
                <WSClientForm
                    existing={clients}
                    onDone={(created) => { setShowAdd(false); if (created) void refresh(); }}
                />
            )}

            {editing && (() => {
                const c = clients.find((x) => x.id === editing);
                if (!c) return null;
                return (
                    <WSClientForm
                        existing={clients}
                        editing={c}
                        onDone={() => { setEditing(null); void refresh(); }}
                    />
                );
            })()}

            <div className="mcc-sect">Clients</div>
            {statuses.map((c) => {
                const pending = pendingId === c.id;
                const rec = clients.find((x) => x.id === c.id);
                const channel = c.running ? `running · ${formatUptime(c.uptime_secs)}` : 'stopped';
                return (
                    <div key={c.id} className="mcc-inst-row" style={pending ? { borderColor: 'rgba(251,191,36,.5)' } : undefined}>
                        <div className="mcc-card-top">
                            <div>
                                <div className="mcc-name">
                                    {c.name}
                                    <span className={`mcc-pill ${pending ? 'starting' : c.running ? 'run' : 'stop'}`} style={{ marginLeft: 8 }}>
                                        {pending ? 'working…' : channel}
                                    </span>
                                </div>
                                <div className="mcc-meta">
                                    {rec?.source === 'vmess'
                                        ? 'vmess link (offline)'
                                        : `${rec?.base_url ?? c.server ?? ''} · ${rec?.instance || 'legacy instance'}`}
                                </div>
                            </div>
                            <span className="mcc-spacer" />
                            <span className="mcc-bchip http">127.0.0.1:{c.http_port}</span>
                            <div className="mcc-inst-actions">
                                <button className="mcc-btn sm" disabled={pending} onClick={() => setEditing(c.id)}>Edit</button>
                                {c.running ? (
                                    <>
                                        <button className="mcc-btn sm" disabled={pending} onClick={() => void runProbe(c)}>Probe</button>
                                        <button className="mcc-btn sm" disabled={pending} onClick={() => void runStop(c)}>Stop</button>
                                    </>
                                ) : (
                                    <button className="mcc-btn sm mcc-btn-primary" disabled={pending} onClick={() => void runStart(c)}>
                                        {pending ? <><span className="spin" />Starting…</> : 'Start'}
                                    </button>
                                )}
                                <button className="mcc-btn sm mcc-btn-danger" disabled={pending} onClick={() => setConfirmDeleteId(c.id)}>Delete</button>
                            </div>
                        </div>

                        <div className="mcc-kv" style={{ marginTop: 8 }}>
                            <span className="k">HTTP inbound</span>
                            <span className="v mcc-mono">127.0.0.1:{c.http_port}</span>
                        </div>
                        <div className="mcc-kv">
                            <span className="k">SOCKS inbound</span>
                            <span className="v mcc-mono">127.0.0.1:{c.socks_port}</span>
                        </div>
                        <div className="mcc-kv">
                            <span className="k">Auto-start</span>
                            <span className="v">{c.auto_start ? 'on' : 'off'}</span>
                        </div>
                        {c.pid ? (
                            <div className="mcc-kv"><span className="k">xray pid</span><span className="v mcc-mono">{c.pid}</span></div>
                        ) : null}
                        {c.last_error && (
                            <div className="mcc-svc-detail-hint" style={{ color: '#fbbf24' }}>last error: {c.last_error}</div>
                        )}

                        <div className="mcc-svc-detail-actions">
                            <button className="mcc-btn sm" onClick={() => { setLogFor(logFor === c.id ? null : c.id); setLogLines([]); }}>Logs</button>
                            <button className="mcc-btn sm" onClick={() => void copyPAC(c)}>Copy PAC</button>
                        </div>

                        {probeResult?.id === c.id && (
                            <div className="mcc-svc-detail-hint" style={{ color: probeResult.ok ? '#34d399' : '#f87171' }}>
                                {probeResult.text}
                            </div>
                        )}

                        {confirmDeleteId === c.id && (
                            <div className="mcc-del-confirm">
                                删除 client <b>{c.name}</b>? 会先 Stop，再删掉本地配置。此操作不可恢复。
                                <div className="mcc-del-confirm-row">
                                    <button className="mcc-btn sm mcc-btn-danger" disabled={pending} onClick={() => void remove(c)}>Delete</button>
                                    <button className="mcc-btn sm" disabled={pending} onClick={() => setConfirmDeleteId(null)}>Cancel</button>
                                </div>
                            </div>
                        )}

                        {logFor === c.id && (
                            <WSProxyLogArea
                                id={c.id}
                                lines={logLines}
                                streaming={pending}
                                step={null}
                                failed={false}
                                onClear={() => setLogLines([])}
                                onHide={() => setLogFor(null)}
                            />
                        )}
                    </div>
                );
            })}
            {!statuses.length && !error && (
                <div className="mcc-ports-empty">No clients yet — add one above.</div>
            )}
        </>
    );
}

/** WSClientForm creates or edits one client. */
function WSClientForm({ existing, editing, onDone }: {
    existing: WSClientRecord[];
    editing?: WSClientRecord;
    onDone: (created: WSClientRecord | null) => void;
}) {
    const [name, setName] = useState(editing?.name ?? '');
    const [source, setSource] = useState<'remote' | 'vmess'>(editing?.source ?? 'remote');
    const [baseURL, setBaseURL] = useState(editing?.base_url ?? '');
    const [token, setToken] = useState('');
    const [instance, setInstance] = useState(editing?.instance ?? '');
    const [vmessLink, setVmessLink] = useState('');
    const [httpPort, setHttpPort] = useState(String(editing?.http_port ?? 10809));
    const [socksPort, setSocksPort] = useState(String(editing?.socks_port ?? 10808));
    const [autoStart, setAutoStart] = useState(editing?.auto_start ?? true);
    const [pacDomains, setPacDomains] = useState((editing?.pac_domains ?? []).join(', '));
    const [error, setError] = useState<string | null>(null);
    const [busy, setBusy] = useState(false);

    const submit = async () => {
        setBusy(true);
        setError(null);
        try {
            const domains = pacDomains.split(',').map((d) => d.trim()).filter(Boolean);
            const payload = {
                name: name.trim(),
                source,
                base_url: source === 'remote' ? baseURL.trim() : '',
                instance: source === 'remote' ? instance.trim() : '',
                token: token.trim() || undefined,
                vmess_link: source === 'vmess' ? vmessLink.trim() : '',
                http_port: Number(httpPort) || undefined,
                socks_port: Number(socksPort) || undefined,
                auto_start: autoStart,
                pac_domains: domains,
            };
            if (editing) {
                await updateWSClient(editing.id, payload);
                onDone(null);
                return;
            }
            const portClash = existing.find((c) =>
                c.http_port === payload.http_port || c.socks_port === payload.socks_port);
            if (portClash) {
                throw new Error(`端口已被 client ${portClash.id} 使用`);
            }
            onDone(await createWSClient(payload));
        } catch (err) {
            setError(err instanceof Error ? err.message : String(err));
        } finally {
            setBusy(false);
        }
    };

    return (
        <div className="mcc-card">
            <div className="mcc-sect" style={{ marginTop: 0 }}>{editing ? `Edit client · ${editing.id}` : 'New client'}</div>
            {error && <div className="mcc-svc-detail-error">{error}</div>}

            <label className="mcc-lbl">名称</label>
            <input className="mcc-input" value={name} onChange={(e) => setName(e.target.value)} placeholder="work-mac" />

            <label className="mcc-lbl">VMess 来源</label>
            <div className={`mcc-backend ${source === 'remote' ? 'sel' : ''}`} onClick={() => setSource('remote')}>
                <span className="mcc-radio" />
                <div>
                    <div className="mcc-bname">从远端 Server 拉取</div>
                    <div className="mcc-bdesc">填 server URL + token + instance；server 改了域名或 UUID 会自动跟随</div>
                </div>
            </div>
            <div className={`mcc-backend ${source === 'vmess' ? 'sel' : ''}`} onClick={() => setSource('vmess')}>
                <span className="mcc-radio" />
                <div>
                    <div className="mcc-bname">粘贴 vmess:// 链接</div>
                    <div className="mcc-bdesc">离线可用；server 侧变化后需手动更新</div>
                </div>
            </div>

            {source === 'remote' ? (
                <>
                    <label className="mcc-lbl">Server base URL</label>
                    <input className="mcc-input" value={baseURL} onChange={(e) => setBaseURL(e.target.value)}
                        placeholder="https://mac-agent.example.com" />
                    <label className="mcc-lbl">Token{editing?.has_token ? '（留空 = 保持不变）' : ''}</label>
                    <input className="mcc-input" type="password" value={token} onChange={(e) => setToken(e.target.value)}
                        placeholder={editing?.has_token ? '••••••••' : 'server token'} />
                    <label className="mcc-lbl">Remote instance</label>
                    <input className="mcc-input" value={instance} onChange={(e) => setInstance(e.target.value)}
                        placeholder="ws-on-mac-aes312" />
                </>
            ) : (
                <>
                    <label className="mcc-lbl">vmess:// link</label>
                    <input className="mcc-input" value={vmessLink} onChange={(e) => setVmessLink(e.target.value)}
                        placeholder="vmess://eyJ2IjoiMiIs…" />
                </>
            )}

            <label className="mcc-lbl">HTTP inbound port</label>
            <input className="mcc-input" value={httpPort} onChange={(e) => setHttpPort(e.target.value)} />
            <label className="mcc-lbl">SOCKS inbound port</label>
            <input className="mcc-input" value={socksPort} onChange={(e) => setSocksPort(e.target.value)} />
            <label className="mcc-lbl">PAC 域名（逗号分隔，留空 = 全部走隧道）</label>
            <input className="mcc-input" value={pacDomains} onChange={(e) => setPacDomains(e.target.value)}
                placeholder="example.com, example.org" />

            <div className="mcc-kv" style={{ marginTop: 10 }}>
                <span className="k">随 ai-critic 启动</span>
                <span><input type="checkbox" checked={autoStart} onChange={(e) => setAutoStart(e.target.checked)} /></span>
            </div>

            <div className="mcc-svc-detail-actions">
                <button className="mcc-btn mcc-btn-primary" onClick={submit} disabled={busy || !name.trim()}>
                    {editing ? 'Save' : 'Create'}
                </button>
                {!editing && <button className="mcc-btn" onClick={submit} disabled={busy || !name.trim()}>Create &amp; Start</button>}
                <button className="mcc-btn" onClick={() => onDone(null)}>Cancel</button>
            </div>
        </div>
    );
}
