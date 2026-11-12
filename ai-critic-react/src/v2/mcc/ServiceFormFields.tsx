import { useEffect, useState } from 'react';
import { fetchOwnedDomains } from '../../api/cloudflare';
import type { ServicePreset, ServiceStatus } from '../../api/services';
import type { ProviderInfo } from '../../hooks/usePortForwards';
import { TunnelProviders } from '../../hooks/usePortForwards';
import { ServiceAuthFields } from './ServiceAuthFields';
import type { ServiceFormState } from './serviceFormState';

export interface ServiceFormFieldsProps {
    form: ServiceFormState;
    onChange: (updater: (prev: ServiceFormState) => ServiceFormState) => void;
    homeDir: string;
    providers: ProviderInfo[];
    /** Presets prefill the form; nothing is created until the user saves. */
    presets: ServicePreset[];
    onApplyPreset: (preset: ServicePreset) => void;
    /** The service being edited, so start-time settings can flag a pending restart. */
    service?: ServiceStatus;
}

/**
 * ServiceFormFields is the body of the add/edit service dialog: every field,
 * with no chrome and no submit button. The modal owns those.
 */
export function ServiceFormFields({ form, onChange, homeDir, providers, presets, onApplyPreset, service }: ServiceFormFieldsProps) {
    // Lifetime is frozen when a process starts, so the checkbox can disagree
    // with the process that is running right now. Say so instead of letting the
    // save look like it already took effect.
    const runningDetached = service && service.status === 'running' ? !!service.detachedActive : undefined;
    const lifetimePending = runningDetached !== undefined && runningDetached !== form.detached;

    return (
        <>
            {!form.id && presets.length > 0 && (
                <div className="mcc-service-presets">
                    <label>Start from a preset</label>
                    <div className="mcc-service-preset-list">
                        {presets.map((preset) => (
                            <button
                                key={preset.id}
                                type="button"
                                className="mcc-service-preset-btn"
                                title={preset.description}
                                onClick={() => onApplyPreset(preset)}
                            >
                                {preset.name}
                            </button>
                        ))}
                    </div>
                </div>
            )}

            <div className="mcc-service-form-grid">
                <div className="mcc-form-field">
                    <label>Name</label>
                    <input
                        type="text"
                        placeholder="web"
                        value={form.name}
                        onChange={(e) => onChange((prev) => ({ ...prev, name: e.target.value }))}
                    />
                </div>
                <div className="mcc-form-field">
                    <label>Command</label>
                    <input
                        type="text"
                        placeholder="npm run dev"
                        value={form.command}
                        onChange={(e) => onChange((prev) => ({ ...prev, command: e.target.value }))}
                    />
                </div>
                <div className="mcc-form-field">
                    <label>Working Dir</label>
                    <input
                        type="text"
                        placeholder={homeDir || '/home/user'}
                        value={form.workingDir}
                        onChange={(e) => onChange((prev) => ({ ...prev, workingDir: e.target.value }))}
                    />
                </div>
                <div className="mcc-form-field">
                    <label>Extra Env</label>
                    <textarea
                        className="mcc-service-env-input"
                        placeholder={'NODE_ENV=development\nPORT=3000'}
                        value={form.extraEnvText}
                        onChange={(e) => onChange((prev) => ({ ...prev, extraEnvText: e.target.value }))}
                        rows={4}
                    />
                    <div className="mcc-service-field-hint">One <code>KEY=VALUE</code> entry per line.</div>
                </div>
                <div className="mcc-form-field">
                    <label>Extra PATH</label>
                    <input
                        type="text"
                        placeholder="~/.local/bin:~/go/bin"
                        value={form.extraPath}
                        onChange={(e) => onChange((prev) => ({ ...prev, extraPath: e.target.value }))}
                    />
                    <div className="mcc-service-field-hint">
                        Absolute directories <strong>prepended</strong> to <code>PATH</code>, separated by <code>:</code>.
                        <code>~</code> is expanded; variables are not.
                    </div>
                </div>
                <div className="mcc-form-field">
                    <label htmlFor="mcc-service-inherit-shell-env">Interactive Shell Env</label>
                    <label className="mcc-service-inherit" htmlFor="mcc-service-inherit-shell-env">
                        <input
                            id="mcc-service-inherit-shell-env"
                            type="checkbox"
                            checked={form.inheritShellEnv}
                            onChange={(e) => {
                                const checked = e.target.checked;
                                onChange((prev) => ({ ...prev, inheritShellEnv: checked }));
                            }}
                        />
                        <span>Inherit interactive shell env</span>
                    </label>
                    <div className="mcc-service-field-hint">
                        Runs <code>$SHELL -lic 'env -0'</code> once at start and merges the result, so exports from{' '}
                        <code>~/.zshrc</code> / <code>~/.bashrc</code> apply. Shell values win over the process env;
                        a failure is reported on the service instead of blocking the start.
                    </div>
                </div>
                <div className="mcc-form-field">
                    <label htmlFor="mcc-service-detached">Lifetime</label>
                    <label className="mcc-service-detach" htmlFor="mcc-service-detached">
                        <input
                            id="mcc-service-detached"
                            type="checkbox"
                            checked={form.detached}
                            onChange={(e) => {
                                const checked = e.target.checked;
                                onChange((prev) => ({ ...prev, detached: checked }));
                            }}
                        />
                        <span>Detached — keep running after ai-critic quits</span>
                    </label>
                    <div className="mcc-service-field-hint">
                        Applies on the next start. A running service keeps its current lifetime until you restart it;
                        Detached services are adopted on the next boot.
                    </div>
                    {lifetimePending && (
                        <div className="mcc-service-lifetime-notice" role="status">
                            {form.detached
                                ? 'This service is running attached. Restart it to keep it running after ai-critic quits.'
                                : 'This service is running detached, so it still survives quitting ai-critic. Restart it to apply the new lifetime.'}
                        </div>
                    )}
                </div>
                <div className="mcc-form-field">
                    <label>Upgrade Pre-Stop Steps</label>
                    <textarea
                        className="mcc-service-env-input"
                        placeholder={'git fetch --all\ngo build -o /tmp/app.new ./cmd/app'}
                        value={form.upgradePreStopText}
                        onChange={(e) => onChange((prev) => ({ ...prev, upgradePreStopText: e.target.value }))}
                        rows={3}
                    />
                    <div className="mcc-service-field-hint">
                        One shell step per line, run by <code>service upgrade</code> while the service is still
                        running. The first failure aborts and leaves the service untouched.
                    </div>
                </div>
                <div className="mcc-form-field">
                    <label>Upgrade Post-Stop Steps</label>
                    <textarea
                        className="mcc-service-env-input"
                        placeholder={'mv /tmp/app.new "$REMOTE_AGENT_UPGRADE_TARGET"'}
                        value={form.upgradePostStopText}
                        onChange={(e) => onChange((prev) => ({ ...prev, upgradePostStopText: e.target.value }))}
                        rows={3}
                    />
                    <div className="mcc-service-field-hint">
                        One shell step per line, run after the service stops. A failure restarts the service
                        before reporting.
                    </div>
                </div>
                <div className="mcc-form-field">
                    <label>Upgrade Step Timeout</label>
                    <input
                        type="text"
                        placeholder="10m"
                        value={form.upgradeTimeout}
                        onChange={(e) => onChange((prev) => ({ ...prev, upgradeTimeout: e.target.value }))}
                    />
                    <div className="mcc-service-field-hint">
                        Per-step limit such as <code>10m</code>; <code>0</code> disables it. Empty uses 15m.
                    </div>
                </div>
            </div>

            <button
                type="button"
                className={`mcc-service-toggle ${form.enablePortForward ? 'active' : ''}`}
                onClick={() => onChange((prev) => ({ ...prev, enablePortForward: !prev.enablePortForward }))}
            >
                {form.enablePortForward ? 'Port forwarding enabled' : 'Add port forwarding'}
            </button>

            {form.enablePortForward && (
                <>
                    <div className="mcc-add-port-fields">
                        <div className="mcc-form-field">
                            <label>Service Port</label>
                            <input
                                type="number"
                                placeholder="3000"
                                value={form.port}
                                onChange={(e) => onChange((prev) => ({ ...prev, port: e.target.value }))}
                            />
                        </div>
                        <div className="mcc-form-field">
                            <label>Forward Label</label>
                            <input
                                type="text"
                                placeholder="app.example.com"
                                value={form.label}
                                onChange={(e) => onChange((prev) => ({ ...prev, label: e.target.value }))}
                            />
                        </div>
                    </div>
                    <div className="mcc-form-field mcc-provider-field">
                        <label>Provider</label>
                        <div className="mcc-provider-options">
                            {providers.map((provider) => (
                                <button
                                    key={provider.id}
                                    className={`mcc-provider-btn ${form.provider === provider.id ? 'active' : ''}`}
                                    onClick={() => onChange((prev) => ({ ...prev, provider: provider.id as ServiceFormState['provider'] }))}
                                    title={provider.description}
                                    type="button"
                                >
                                    {provider.name}
                                </button>
                            ))}
                        </div>
                    </div>
                    {(form.provider === TunnelProviders.CloudflareTunnel || form.provider === TunnelProviders.CloudflareOwned) && (
                        <>
                            <OwnedDomainsPicker
                                selectedDomain={form.baseDomain}
                                onSelect={(domain) => onChange((prev) => ({ ...prev, baseDomain: domain }))}
                            />
                            <div className="mcc-form-field mcc-subdomain-field">
                                <label>Subdomain</label>
                                <input
                                    type="text"
                                    placeholder="brave-apex-dawn"
                                    value={form.subdomain}
                                    onChange={(e) => onChange((prev) => ({ ...prev, subdomain: e.target.value }))}
                                    className="mcc-subdomain-input"
                                />
                            </div>
                        </>
                    )}
                </>
            )}

            <ServiceAuthFields
                requireAuth={form.requireAuth}
                authUserMode={form.authUserMode}
                authUser={form.authUser}
                authTokenMode={form.authTokenMode}
                authToken={form.authToken}
                onRequireAuthChange={(requireAuth) => onChange((prev) => ({ ...prev, requireAuth }))}
                onAuthUserModeChange={(authUserMode) => onChange((prev) => ({ ...prev, authUserMode }))}
                onAuthUserChange={(authUser) => onChange((prev) => ({ ...prev, authUser }))}
                onAuthTokenModeChange={(authTokenMode) => onChange((prev) => ({ ...prev, authTokenMode }))}
                onAuthTokenChange={(authToken) => onChange((prev) => ({ ...prev, authToken }))}
            />
        </>
    );
}

interface OwnedDomainsPickerProps {
    selectedDomain: string;
    onSelect: (domain: string) => void;
}

function OwnedDomainsPicker({ selectedDomain, onSelect }: OwnedDomainsPickerProps) {
    const [domains, setDomains] = useState<string[]>([]);

    useEffect(() => {
        fetchOwnedDomains()
            .then((items) => {
                setDomains(items);
                if (items.length > 0 && !selectedDomain) {
                    onSelect(items[0]);
                }
            })
            .catch(() => {});
    }, []);

    if (domains.length === 0) return null;

    return (
        <div className="mcc-owned-domains-hint">
            <label>Base Domain</label>
            <div className="mcc-owned-domains-list">
                {domains.map((domain) => (
                    <button
                        key={domain}
                        className={`mcc-owned-domain-btn ${selectedDomain === domain ? 'active' : ''}`}
                        onClick={() => onSelect(domain)}
                        type="button"
                    >
                        {domain}
                    </button>
                ))}
            </div>
        </div>
    );
}
