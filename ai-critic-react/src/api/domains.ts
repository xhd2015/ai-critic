// Domains API client

export const DomainProviders = {
    Cloudflare: "cloudflare",
    Ngrok: "ngrok",
} as const;

export type DomainProvider = typeof DomainProviders[keyof typeof DomainProviders];

export interface DomainEntry {
    domain: string;
    provider: string;
}

export interface DomainWithStatus extends DomainEntry {
    status: string; // "stopped" | "connecting" | "active" | "error"
    tunnel_url?: string;
    error?: string;
}

export interface DomainsConfig {
    domains: DomainEntry[];
}

export interface DomainsWithStatusResponse {
    domains: DomainWithStatus[];
}

export interface CloudflareStatus {
    installed: boolean;
    authenticated: boolean;
    auth_error?: string;
}

// Tunnel edge transports understood by cloudflared --protocol.
export const TunnelProtocols = {
    HTTP2: 'http2',
    QUIC: 'quic',
} as const;

export type TunnelProtocol = typeof TunnelProtocols[keyof typeof TunnelProtocols];

export interface TunnelProtocolInfo {
    /** Persisted value; '' means "not configured". */
    protocol: string;
    /** What cloudflared is actually launched with. */
    effective: string;
    /** The built-in fallback. */
    default: string;
}

export interface TunnelProtocolSaveResult extends TunnelProtocolInfo {
    restarted: boolean;
}

export async function fetchTunnelProtocol(): Promise<TunnelProtocolInfo> {
    const resp = await fetch('/api/domains/tunnel-protocol');
    if (!resp.ok) {
        const data = await resp.json().catch(() => ({}));
        throw new Error(data.error || 'Failed to fetch edge protocol');
    }
    return resp.json();
}

export async function saveTunnelProtocol(protocol: string, restart = true): Promise<TunnelProtocolSaveResult> {
    const resp = await fetch('/api/domains/tunnel-protocol', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ protocol, no_restart: !restart }),
    });
    if (!resp.ok) {
        const data = await resp.json().catch(() => ({}));
        throw new Error(data.error || 'Failed to save edge protocol');
    }
    return resp.json();
}

export async function fetchDomains(): Promise<DomainsWithStatusResponse> {
    const resp = await fetch('/api/domains');
    if (!resp.ok) {
        const data = await resp.json().catch(() => ({}));
        throw new Error(data.error || 'Failed to fetch domains');
    }
    return resp.json();
}

export async function saveDomains(config: DomainsConfig): Promise<void> {
    const resp = await fetch('/api/domains', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(config),
    });
    if (!resp.ok) {
        const data = await resp.json().catch(() => ({}));
        throw new Error(data.error || 'Failed to save domains');
    }
}

export async function fetchCloudflareStatus(): Promise<CloudflareStatus> {
    const resp = await fetch('/api/domains/cloudflare-status');
    if (!resp.ok) {
        const data = await resp.json().catch(() => ({}));
        throw new Error(data.error || 'Failed to fetch cloudflare status');
    }
    return resp.json();
}

/** Start a tunnel, returns raw Response for SSE streaming. */
export async function startTunnel(domain: string): Promise<Response> {
    const resp = await fetch('/api/domains/tunnel/start', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ domain }),
    });
    if (!resp.ok) {
        const data = await resp.json().catch(() => ({}));
        throw new Error(data.error || 'Failed to start tunnel');
    }
    return resp;
}

export async function fetchRandomDomain(current?: string): Promise<string> {
    const params = current ? `?current=${encodeURIComponent(current)}` : '';
    const resp = await fetch(`/api/domains/random-subdomain${params}`);
    if (!resp.ok) {
        const data = await resp.json().catch(() => ({}));
        throw new Error(data.error || 'Failed to generate domain');
    }
    const data = await resp.json();
    return data.domain;
}

export async function stopTunnel(domain: string): Promise<void> {
    const resp = await fetch('/api/domains/tunnel/stop', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ domain }),
    });
    if (!resp.ok) {
        const data = await resp.json().catch(() => ({}));
        throw new Error(data.error || 'Failed to stop tunnel');
    }
}

export async function fetchTunnelName(): Promise<string> {
    const resp = await fetch('/api/domains/tunnel-name');
    if (!resp.ok) {
        const data = await resp.json().catch(() => ({}));
        throw new Error(data.error || 'Failed to fetch tunnel name');
    }
    const data = await resp.json();
    return data.tunnel_name || '';
}

export async function saveTunnelName(tunnelName: string): Promise<void> {
    const resp = await fetch('/api/domains/tunnel-name', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ tunnel_name: tunnelName }),
    });
    if (!resp.ok) {
        const data = await resp.json().catch(() => ({}));
        throw new Error(data.error || 'Failed to save tunnel name');
    }
}
