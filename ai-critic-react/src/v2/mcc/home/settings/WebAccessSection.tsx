import { useState, useEffect } from 'react';
import { fetchDomains, saveDomains, fetchCloudflareStatus, startTunnel, stopTunnel, fetchTunnelName, saveTunnelName, fetchTunnelProtocol, saveTunnelProtocol } from '../../../../api/domains';
import type { DomainEntry, DomainWithStatus, CloudflareStatus, TunnelProtocolInfo } from '../../../../api/domains';
import { consumeSSEStream } from '../../../../api/sse';
import type { LogLine } from '../../../LogViewer';
import { DomainRowView } from './DomainRowView';
import { DomainRowEdit } from './DomainRowEdit';
import { AddDomainForm } from './AddDomainForm';
import { Loading } from '../../../../pure-view/Loading';
import { Section } from '../../../../pure-view/Section';
import { InlineError } from '../../../../pure-view/InlineError';
import { EdgeProtocolField } from './EdgeProtocolField';
import './WebAccessSection.css';

export function WebAccessSection() {
    const [domainsList, setDomainsList] = useState<DomainWithStatus[]>([]);
    const [cfStatus, setCfStatus] = useState<CloudflareStatus | null>(null);
    const [loading, setLoading] = useState(true);
    const [error, setError] = useState<string | null>(null);

    // Per-domain edit state: index of the domain being edited, or null
    const [editingIndex, setEditingIndex] = useState<number | null>(null);

    // Persisted tunnel name
    const [tunnelName, setTunnelName] = useState('');

    // Edge transport (http2 default, quic optional)
    const [protocolInfo, setProtocolInfo] = useState<TunnelProtocolInfo | null>(null);
    const [savingProtocol, setSavingProtocol] = useState(false);

    // Per-domain tunnel start streaming state
    const [startingDomain, setStartingDomain] = useState<string | null>(null);
    const [startLogs, setStartLogs] = useState<LogLine[]>([]);
    const [startDone, setStartDone] = useState(false);
    const [startError, setStartError] = useState(false);

    // Add domain form
    const [showAddForm, setShowAddForm] = useState(false);

    const loadData = () => {
        setLoading(true);
        setError(null);
        Promise.all([fetchDomains(), fetchTunnelName(), fetchTunnelProtocol().catch(() => null)])
            .then(([domainsResp, tn, protocol]) => {
                setDomainsList(domainsResp.domains ?? []);
                setTunnelName(tn);
                setProtocolInfo(protocol);
                setLoading(false);
            })
            .catch(err => { setError(err.message); setLoading(false); });

        fetchCloudflareStatus()
            .then(cfResp => setCfStatus(cfResp))
            .catch(() => {});
    };

    useEffect(() => {
        const hasConnecting = domainsList.some(d => d.status === 'connecting');
        if (!hasConnecting) return;
        const timer = setInterval(() => {
            fetchDomains()
                .then(resp => setDomainsList(resp.domains ?? []))
                .catch(() => {});
        }, 3000);
        return () => clearInterval(timer);
    }, [domainsList]);

    useEffect(() => { loadData(); }, []);

    const handleSaveDomain = async (index: number, entry: DomainEntry, newTunnelName: string) => {
        setError(null);
        try {
            const entries = domainsList.map(d => ({ domain: d.domain, provider: d.provider }));
            entries[index] = entry;
            await saveDomains({ domains: entries });
            if (newTunnelName !== tunnelName) {
                await saveTunnelName(newTunnelName);
                setTunnelName(newTunnelName);
            }
            const resp = await fetchDomains();
            setDomainsList(resp.domains ?? []);
            setEditingIndex(null);
        } catch (err) {
            setError(err instanceof Error ? err.message : String(err));
        }
    };

    const handleRemoveDomain = async (index: number) => {
        setError(null);
        try {
            const entries = domainsList.map(d => ({ domain: d.domain, provider: d.provider }));
            const updated = entries.filter((_, i) => i !== index);
            await saveDomains({ domains: updated });
            const resp = await fetchDomains();
            setDomainsList(resp.domains ?? []);
            setEditingIndex(null);
        } catch (err) {
            setError(err instanceof Error ? err.message : String(err));
        }
    };

    const handleSaveProtocol = async (protocol: string, restart: boolean) => {
        setError(null);
        setSavingProtocol(true);
        try {
            const res = await saveTunnelProtocol(protocol, restart);
            setProtocolInfo({ protocol: res.protocol, effective: res.effective, default: res.default });
        } catch (err) {
            setError(err instanceof Error ? err.message : String(err));
        }
        setSavingProtocol(false);
    };

    const handleAddDomain = async (entry: DomainEntry) => {
        setError(null);
        try {
            const entries = domainsList.map(d => ({ domain: d.domain, provider: d.provider }));
            entries.push(entry);
            await saveDomains({ domains: entries });
            const resp = await fetchDomains();
            setDomainsList(resp.domains ?? []);
            setShowAddForm(false);
        } catch (err) {
            setError(err instanceof Error ? err.message : String(err));
        }
    };

    const handleStart = async (domain: string) => {
        setError(null);
        setStartingDomain(domain);
        setStartLogs([]);
        setStartDone(false);
        setStartError(false);
        try {
            const resp = await startTunnel(domain);
            await consumeSSEStream(resp, {
                onLog: (line) => setStartLogs(prev => [...prev, line]),
                onError: (line) => { setStartLogs(prev => [...prev, line]); setStartError(true); },
                onDone: async (message) => {
                    setStartLogs(prev => [...prev, { text: message }]);
                    setStartDone(true);
                    const domainsResp = await fetchDomains();
                    setDomainsList(domainsResp.domains ?? []);
                },
            });
        } catch (err) {
            setStartLogs(prev => [...prev, { text: String(err), error: true }]);
            setStartError(true);
        }
        setStartingDomain(null);
    };

    const handleStop = async (domain: string) => {
        setError(null);
        try {
            await stopTunnel(domain);
            const resp = await fetchDomains();
            setDomainsList(resp.domains ?? []);
        } catch (err) {
            setError(err instanceof Error ? err.message : String(err));
        }
    };

    const isStarting = (domain: string) => startingDomain === domain;
    const displayTunnelName = tunnelName || '(auto)';

    return (
        <Section title="Web Access">

            {loading ? (
                <Loading>Loading domains...</Loading>
            ) : (
                <div className="webaccess-card">
                    {domainsList.length === 0 && !showAddForm && (
                        <div className="webaccess-empty">
                            No domains configured. Add one to enable public access.
                        </div>
                    )}

                    {domainsList.length > 0 && (
                        <div className="webaccess-list">
                            {domainsList.map((entry, i) =>
                                editingIndex === i ? (
                                    <DomainRowEdit
                                        key={i}
                                        entry={{ domain: entry.domain, provider: entry.provider }}
                                        tunnelName={tunnelName}
                                        onSave={(updated, newTn) => handleSaveDomain(i, updated, newTn)}
                                        onRemove={() => handleRemoveDomain(i)}
                                        onCancel={() => setEditingIndex(null)}
                                        onError={setError}
                                    />
                                ) : (
                                    <DomainRowView
                                        key={entry.domain}
                                        entry={entry}
                                        cfStatus={cfStatus}
                                        displayTunnelName={displayTunnelName}
                                        starting={isStarting(entry.domain)}
                                        startingDomain={startingDomain}
                                        startLogs={startLogs}
                                        startDone={startDone}
                                        startError={startError}
                                        isLast={i === domainsList.length - 1}
                                        onStart={handleStart}
                                        onStop={handleStop}
                                        onEdit={() => setEditingIndex(i)}
                                    />
                                )
                            )}
                        </div>
                    )}

                    <EdgeProtocolField
                        info={protocolInfo}
                        saving={savingProtocol}
                        onSave={handleSaveProtocol}
                        onError={setError}
                    />

                    {error && <InlineError>{error}</InlineError>}

                    {showAddForm ? (
                        <AddDomainForm
                            onAdd={handleAddDomain}
                            onCancel={() => setShowAddForm(false)}
                            onError={setError}
                        />
                    ) : (
                        <button className="webaccess-add-toggle" onClick={() => setShowAddForm(true)}>
                            + Add Domain
                        </button>
                    )}
                </div>
            )}
        </Section>
    );
}
