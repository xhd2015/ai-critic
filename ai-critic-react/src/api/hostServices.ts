// Host-level systemd services: which units boot, which run, and who really
// owns the process behind them.

export type HostServiceOwner = 'systemd' | 'ai-critic' | 'both' | 'unused';

export interface HostService {
    unit: string;
    boot: string;
    active: string;
    sub?: string;
    description?: string;
    owner: HostServiceOwner;
    managed_by_ai_critic?: string;
    protected?: boolean;
}

export interface HostServicesSummary {
    total: number;
    unused: number;
    both: number;
}

export interface HostServicesResponse {
    services: HostService[];
    summary: HostServicesSummary;
}

export interface HostServiceDisableResult {
    unit: string;
    boot: string;
    masked: boolean;
    message?: string;
}

export async function fetchHostServices(): Promise<HostServicesResponse> {
    const resp = await fetch('/api/host/services');
    if (!resp.ok) {
        const data = await resp.json().catch(() => ({}));
        throw new Error(data.error || 'Failed to list host services');
    }
    return resp.json();
}

export async function disableHostService(unit: string, mask: boolean): Promise<HostServiceDisableResult> {
    const resp = await fetch('/api/host/services/disable', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ unit, mask }),
    });
    if (!resp.ok) {
        const data = await resp.json().catch(() => ({}));
        throw new Error(data.error || 'Failed to disable host service');
    }
    return resp.json();
}
