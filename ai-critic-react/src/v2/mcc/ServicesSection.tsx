import { useEffect, useMemo, useState } from 'react';
import { useNavigate } from 'react-router-dom';
import { fetchHomeDir } from '../../api/files';
import { deleteService, disableService, enableService, fetchServicePresets, fetchServices, isSystemService, restartService, saveService, startService, stopService, type ServiceDefinition, type ServicePreset, type ServiceStatus } from '../../api/services';
import type { ProviderInfo } from '../../hooks/usePortForwards';
import { ConfirmModal } from './ConfirmModal';
import { ServiceCard } from './ServiceCard';
import { ServiceFormModal } from './ServiceFormModal';
import { AddServiceButton } from './AddServiceButton';
import { SystemServiceCard } from './SystemServiceCard';
import { createDefaultForm, parseUpgradeSteps, parseUpgradeTimeoutInput, toFormState, type ServiceFormState } from './serviceFormState';
import { disableMessage, enableMessage, parseEnvText } from './serviceFormat';

interface ServicesSectionProps {
    availableProviders: ProviderInfo[];
}

/**
 * serviceConfirmInfo describes a service inside a confirm modal. System
 * services have no command, so they report their kind instead.
 */
function serviceConfirmInfo(service: ServiceStatus): Record<string, string> {
    if (isSystemService(service)) {
        return { Name: service.name, Kind: 'system service' };
    }
    return { Name: service.name, Command: service.command };
}

export function ServicesSection({ availableProviders }: ServicesSectionProps) {
    const navigate = useNavigate();
    const openServicePage = (view: string) => navigate(view);
    const [homeDir, setHomeDir] = useState('');
    const [services, setServices] = useState<ServiceStatus[]>([]);
    const [presets, setPresets] = useState<ServicePreset[]>([]);
    const [loading, setLoading] = useState(true);
    const [error, setError] = useState<string | null>(null);
    const [actionError, setActionError] = useState<string | null>(null);
    const [showForm, setShowForm] = useState(false);
    const [saving, setSaving] = useState(false);
    const [form, setForm] = useState<ServiceFormState>(createDefaultForm());
    const [lifetimeNotice, setLifetimeNotice] = useState<string | null>(null);
    const [deleteTarget, setDeleteTarget] = useState<ServiceStatus | null>(null);
    const [disableTarget, setDisableTarget] = useState<ServiceStatus | null>(null);
    const [enableTarget, setEnableTarget] = useState<ServiceStatus | null>(null);

    const providerButtons = useMemo(
        () => availableProviders.filter((provider) => provider.available),
        [availableProviders],
    );

    // User services come first, matching the section order below.
    const userServices = useMemo(() => services.filter((service) => !isSystemService(service)), [services]);
    const systemServices = useMemo(() => services.filter(isSystemService), [services]);

    const refreshServices = async () => {
        try {
            const data = await fetchServices();
            setServices(data);
            setError(null);
        } catch (err) {
            setError(err instanceof Error ? err.message : String(err));
        } finally {
            setLoading(false);
        }
    };

    useEffect(() => {
        setLoading(true);
        refreshServices();
        const timer = setInterval(refreshServices, 3000);
        return () => clearInterval(timer);
    }, []);

    useEffect(() => {
        if (!lifetimeNotice) return;
        const timer = setTimeout(() => setLifetimeNotice(null), 12000);
        return () => clearTimeout(timer);
    }, [lifetimeNotice]);

    useEffect(() => {
        fetchHomeDir()
            .then((dir) => setHomeDir(dir))
            .catch(() => {});
    }, []);

    useEffect(() => {
        fetchServicePresets()
            .then((items) => setPresets(items))
            .catch(() => {});
    }, []);

    const resetForm = () => {
        setForm(createDefaultForm(homeDir));
        setShowForm(false);
        setActionError(null);
    };

    /** The service behind the open form, kept live by the poll. */
    const editingService = form.id ? services.find((service) => service.id === form.id) : undefined;

    const openNewForm = () => {
        setForm(createDefaultForm(homeDir));
        setActionError(null);
        setShowForm(true);
    };

    const openEditForm = (service: ServiceStatus) => {
        setForm(toFormState(service));
        setActionError(null);
        setShowForm(true);
    };

    const applyPreset = (preset: ServicePreset) => {
        setForm((prev) => ({
            ...prev,
            name: preset.name,
            command: preset.command,
            port: preset.port ? String(preset.port) : prev.port,
            enablePortForward: preset.port ? true : prev.enablePortForward,
        }));
    };

    const handleSave = async () => {
        const name = form.name.trim();
        const command = form.command.trim();
        const workingDir = form.workingDir.trim();
        if (!name || !command) {
            setActionError('Name and command are required.');
            return;
        }

        const parsedEnv = parseEnvText(form.extraEnvText);
        if (parsedEnv.error) {
            setActionError(parsedEnv.error);
            return;
        }

        // The server re-validates and normalises; catch the obvious mistakes
        // here so the dialog can report them without a round trip.
        const extraPath = form.extraPath.trim();
        if (/[\r\n]/.test(form.extraPath)) {
            setActionError('Extra PATH must be a single line of directories separated by ":".');
            return;
        }
        if (extraPath.includes('$')) {
            setActionError('Extra PATH does not expand variables; use a literal directory instead.');
            return;
        }
        if (extraPath) {
            const dirs = extraPath.split(':');
            if (dirs.some((dir) => !dir.trim())) {
                setActionError('Extra PATH has an empty entry; separate directories with ":".');
                return;
            }
            const relative = dirs
                .map((dir) => dir.trim())
                .find((dir) => !dir.startsWith('/') && !dir.startsWith('~'));
            if (relative) {
                setActionError(`Extra PATH directory must be absolute: ${relative}`);
                return;
            }
        }

        let portForward: ServiceDefinition['portForward'];
        if (form.enablePortForward) {
            const port = Number.parseInt(form.port, 10);
            if (!port || port < 1 || port > 65535) {
                setActionError('A valid service port is required.');
                return;
            }
            portForward = {
                port,
                label: form.label.trim() || undefined,
                provider: form.provider,
                baseDomain: form.baseDomain.trim() || undefined,
                subdomain: form.subdomain.trim() || undefined,
            };
        }

        if (form.requireAuth) {
            if (!portForward) {
                setActionError('Require Auth needs port forwarding.');
                return;
            }
            if (form.authUserMode === 'fixed' && !form.authUser.trim()) {
                setActionError('A username is required for fixed auth user.');
                return;
            }
            if (form.authTokenMode === 'custom' && !form.authToken.trim()) {
                setActionError('A custom auth token is required.');
                return;
            }
        }

        // Detached only takes effect on the next start, so remember how the
        // running process was started before the save changes the definition.
        const previous = editingService;

        setSaving(true);
        setActionError(null);
        try {
            const upgradeTimeout = parseUpgradeTimeoutInput(form.upgradeTimeout);
            if (upgradeTimeout.error) {
                setActionError(upgradeTimeout.error);
                setSaving(false);
                return;
            }
            await saveService({
                id: form.id,
                name,
                command,
                workingDir: workingDir || undefined,
                extraEnv: parsedEnv.env,
                extraPath: extraPath || undefined,
                inheritInteractiveShellEnv: form.inheritShellEnv,
                detached: form.detached,
                portForward,
                // Carried through so saving the form cannot silently drop the
                // configured upgrade steps.
                upgradePreStopCmds: parseUpgradeSteps(form.upgradePreStopText),
                upgradePostStopCmds: parseUpgradeSteps(form.upgradePostStopText),
                upgradeTimeoutSeconds: upgradeTimeout.seconds,
                requireAuth: form.requireAuth,
                authUser: form.authUserMode === 'fixed' ? form.authUser.trim() : undefined,
                authTokenMode: form.requireAuth || form.authTokenMode === 'custom' || form.authUserMode === 'fixed' ? form.authTokenMode : undefined,
                authToken: form.authTokenMode === 'custom' ? form.authToken : undefined,
            });
            await refreshServices();
            if (previous && previous.status === 'running' && !!previous.detachedActive !== form.detached) {
                setLifetimeNotice(
                    `${name} is still running with its previous lifetime. Restart it to apply the new setting.`,
                );
            }
            resetForm();
        } catch (err) {
            setActionError(err instanceof Error ? err.message : String(err));
        } finally {
            setSaving(false);
        }
    };

    const handleAction = async (fn: () => Promise<void>) => {
        setActionError(null);
        try {
            await fn();
            await refreshServices();
        } catch (err) {
            setActionError(err instanceof Error ? err.message : String(err));
        }
    };

    return (
        <section className="mcc-service-section">
            <div className="mcc-section-header">
                <h2>Services</h2>
            </div>

            {error && <div className="mcc-ports-error">Error: {error}</div>}
            {/* Save errors belong to the dialog; the page only reports them if
                the dialog is gone (e.g. closed mid-save). */}
            {actionError && !showForm && <div className="mcc-ports-error">{actionError}</div>}

            <div className="mcc-service-group">
                <h3 className="mcc-service-group-title">User Services</h3>
                <div className="mcc-service-subtitle">
                    Define commands the server should keep alive, optionally with managed port forwarding.
                </div>

                {lifetimeNotice && (
                    <div className="mcc-service-notice" role="status">
                        <span>{lifetimeNotice}</span>
                        <button
                            type="button"
                            className="mcc-service-notice-close"
                            aria-label="Dismiss"
                            onClick={() => setLifetimeNotice(null)}
                        >
                            ×
                        </button>
                    </div>
                )}

                <div className="mcc-service-list">
                    {userServices.map((service) => (
                        <ServiceCard
                            key={service.id}
                            service={service}
                            onEdit={() => openEditForm(service)}
                            onStart={() => handleAction(() => startService(service.id))}
                            onStop={() => handleAction(() => stopService(service.id))}
                            onRestart={() => handleAction(() => restartService(service.id))}
                            onDisable={() => setDisableTarget(service)}
                            onEnable={() => setEnableTarget(service)}
                            onDelete={() => setDeleteTarget(service)}
                            onUpgraded={() => { void refreshServices(); }}
                        />
                    ))}
                    {!loading && userServices.length === 0 && (
                        <div className="mcc-ports-empty">No user services configured.</div>
                    )}
                </div>

                <div className="mcc-add-port-section">
                    <AddServiceButton onClick={openNewForm} />
                </div>
            </div>

            <div className="mcc-service-group">
                <h3 className="mcc-service-group-title">System Services</h3>
                <div className="mcc-service-subtitle">
                    Services the server owns and runs in-process; not editable commands.
                </div>

                <div className="mcc-service-list">
                    {systemServices.map((service) => (
                        <SystemServiceCard
                            key={service.id}
                            service={service}
                            onOpen={service.id === 'sys-http-proxy'
                                ? () => openServicePage('http-proxy')
                                : service.id === 'sys-ws-proxy'
                                    ? () => openServicePage('ws-proxy')
                                    : undefined}
                            onStart={() => handleAction(() => startService(service.id))}
                            onStop={() => handleAction(() => stopService(service.id))}
                            onRestart={() => handleAction(() => restartService(service.id))}
                            onDisable={() => setDisableTarget(service)}
                            onEnable={() => setEnableTarget(service)}
                            onRefresh={() => { void refreshServices(); }}
                        />
                    ))}
                    {!loading && systemServices.length === 0 && (
                        <div className="mcc-ports-empty">No system services.</div>
                    )}
                </div>
            </div>

            {deleteTarget && (
                <ConfirmModal
                    title="Delete Service"
                    message="Are you sure you want to delete this service?"
                    info={serviceConfirmInfo(deleteTarget)}
                    command={`delete service ${deleteTarget.id}`}
                    confirmLabel="Delete Service"
                    confirmVariant="danger"
                    onConfirm={async () => {
                        await handleAction(() => deleteService(deleteTarget.id));
                        setDeleteTarget(null);
                    }}
                    onClose={() => setDeleteTarget(null)}
                />
            )}

            {disableTarget && (
                <ConfirmModal
                    title="Disable Service"
                    message={disableMessage(disableTarget)}
                    info={serviceConfirmInfo(disableTarget)}
                    command={`disable service ${disableTarget.id}`}
                    confirmLabel="Disable"
                    confirmVariant="danger"
                    onConfirm={async () => {
                        await handleAction(() => disableService(disableTarget.id).then(() => undefined));
                        setDisableTarget(null);
                    }}
                    onClose={() => setDisableTarget(null)}
                />
            )}

            {enableTarget && (
                <ConfirmModal
                    title="Enable Service"
                    message={enableMessage(enableTarget)}
                    info={serviceConfirmInfo(enableTarget)}
                    command={`enable service ${enableTarget.id}`}
                    confirmLabel="Enable"
                    onConfirm={async () => {
                        await handleAction(() => enableService(enableTarget.id).then(() => undefined));
                        setEnableTarget(null);
                    }}
                    onClose={() => setEnableTarget(null)}
                />
            )}

            {showForm && (
                <ServiceFormModal
                    form={form}
                    onChange={setForm}
                    onSubmit={handleSave}
                    onClose={resetForm}
                    saving={saving}
                    error={actionError}
                    homeDir={homeDir}
                    providers={providerButtons}
                    presets={presets}
                    onApplyPreset={applyPreset}
                    service={editingService}
                />
            )}
        </section>
    );
}
