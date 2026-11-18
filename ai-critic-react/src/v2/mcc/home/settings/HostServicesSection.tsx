import { useEffect, useState } from 'react';
import { fetchHostServices, disableHostService } from '../../../../api/hostServices';
import type { HostService, HostServicesSummary } from '../../../../api/hostServices';
import { Loading } from '../../../../pure-view/Loading';
import { InlineError } from '../../../../pure-view/InlineError';
import { Button } from '../../../../pure-view/buttons/Button';
import './HostServicesSection.css';

const ownerLabels: Record<string, string> = {
    systemd: 'systemd',
    'ai-critic': 'ai-critic',
    both: 'both — duplicate',
    unused: 'unused',
};

/**
 * Boot-enabled systemd units and who really owns each process.
 *
 * The ai-critic service table only knows children it spawned, so a boot-enabled
 * unit can be shadowed by an ai-critic child (cloudflared) or defined twice
 * (llm-proxy) without anything surfacing it.
 */
export function HostServicesSection() {
    const [services, setServices] = useState<HostService[]>([]);
    const [summary, setSummary] = useState<HostServicesSummary | null>(null);
    const [loading, setLoading] = useState(true);
    const [error, setError] = useState<string | null>(null);
    const [busyUnit, setBusyUnit] = useState<string | null>(null);
    const [notice, setNotice] = useState<string | null>(null);

    const load = () => {
        setLoading(true);
        setError(null);
        fetchHostServices()
            .then(resp => {
                setServices(resp.services ?? []);
                setSummary(resp.summary ?? null);
            })
            .catch(err => setError(err instanceof Error ? err.message : String(err)))
            .finally(() => setLoading(false));
    };

    useEffect(() => { load(); }, []);

    const handleDisable = (unit: string) => {
        setBusyUnit(unit);
        setNotice(null);
        setError(null);
        disableHostService(unit, true)
            .then(res => {
                setNotice(`${res.unit}: boot ${res.boot}${res.masked ? ', masked' : ''}`);
                load();
            })
            .catch(err => setError(err instanceof Error ? err.message : String(err)))
            .finally(() => setBusyUnit(null));
    };

    return (
        <div className="host-services">
            {loading && <Loading>Loading host services…</Loading>}

            {!loading && summary && (
                <div className="host-services-summary">
                    {summary.total} boot-enabled units
                    {summary.unused > 0 && <> · <b>{summary.unused}</b> unused</>}
                    {summary.both > 0 && <> · <b>{summary.both}</b> duplicate</>}
                </div>
            )}

            {notice && <div className="host-services-notice">{notice}</div>}
            {error && <InlineError>{error}</InlineError>}

            {!loading && services.length > 0 && (
                <div className="host-services-list">
                    {services.map(s => (
                        <div className={`host-services-row host-services-owner-${s.owner}`} key={s.unit}>
                            <div className="host-services-main">
                                <div className="host-services-unit">{s.unit}</div>
                                <div className="host-services-meta">
                                    {s.description || s.sub || ''}
                                    {s.managed_by_ai_critic && (
                                        <> · ai-critic service: {s.managed_by_ai_critic}</>
                                    )}
                                </div>
                            </div>
                            <div className="host-services-badges">
                                <span className={`host-services-badge host-services-boot-${s.boot}`}>{s.boot}</span>
                                <span className={`host-services-badge host-services-active-${s.active}`}>{s.active}</span>
                                <span className={`host-services-badge host-services-owner-badge-${s.owner}`}>
                                    {ownerLabels[s.owner] || s.owner}
                                </span>
                            </div>
                            <div className="host-services-action">
                                {s.protected ? (
                                    <span className="host-services-protected" title="Runs this server; disabling is refused">
                                        protected
                                    </span>
                                ) : s.owner === 'unused' || s.owner === 'both' ? (
                                    <Button
                                        variant="danger"
                                        disabled={busyUnit === s.unit}
                                        onClick={() => handleDisable(s.unit)}
                                    >
                                        {busyUnit === s.unit ? 'Disabling…' : 'Disable boot'}
                                    </Button>
                                ) : null}
                            </div>
                        </div>
                    ))}
                </div>
            )}
        </div>
    );
}
