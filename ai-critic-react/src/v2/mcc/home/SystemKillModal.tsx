import { useState } from 'react';
import { ConfirmModal } from '../ConfirmModal';
import { formatBytes, killHostProcess, type HostProcess } from '../../../api/processes';
import { KILL_MODES, killModeSignalText, killModeTitle, resolveBatchKillModal, resolveKillModal, type KillMode } from './killMode';

export function SystemKillModal({
    targets,
    onClose,
    onKilled,
}: {
    targets: HostProcess[];
    onClose: () => void;
    onKilled: () => void;
}) {
    const [mode, setMode] = useState<KillMode>('graceful');
    const live = targets.filter(proc => !proc.protected);
    const hasTunnel = live.some(proc => (proc.tags ?? []).includes('tun'));
    const one = targets.length === 1 ? targets[0] : null;
    const state = one
        ? resolveKillModal({
            pid: one.pid,
            mode,
            protectedReason: one.protected,
            isTunnel: hasTunnel,
        })
        : resolveBatchKillModal({ pids: live.map(proc => proc.pid), mode, hasTunnel });

    const info: Record<string, string> = one
        ? {
            Name: one.name,
            PID: String(one.pid),
            User: one.user,
            'CPU / RSS': `${one.cpu_percent.toFixed(1)}% · ${formatBytes(one.rss_bytes)}`,
        }
        : {
            Processes: String(live.length),
            PIDs: live.slice(0, 8).map(proc => String(proc.pid)).join(', ') + (live.length > 8 ? '…' : ''),
        };

    return (
        <ConfirmModal
            title={live.length > 1 ? `Kill ${live.length} processes` : 'Kill Process'}
            message={state.message}
            warning={state.warning}
            info={info}
            command={state.command}
            confirmLabel={state.confirmLabel}
            confirmVariant={state.locked ? 'default' : 'danger'}
            onConfirm={async () => {
                if (state.locked) return;
                const failed: string[] = [];
                for (const proc of live) {
                    try {
                        await killHostProcess(proc.pid, state.signal);
                    } catch (err) {
                        failed.push(`${proc.pid}: ${err instanceof Error ? err.message : String(err)}`);
                    }
                }
                if (failed.length > 0) throw new Error(failed.join('\n'));
                onKilled();
            }}
            onClose={onClose}
        >
            <div className="mcc-modal-modes" role="radiogroup" aria-label="Kill mode">
                {KILL_MODES.map(option => {
                    const active = mode === option;
                    const className = [
                        'mcc-modal-mode',
                        active ? 'mcc-modal-mode-active' : '',
                        option === 'force' ? 'mcc-modal-mode-force' : '',
                    ]
                        .filter(Boolean)
                        .join(' ');
                    return (
                        <button
                            key={option}
                            type="button"
                            role="radio"
                            aria-checked={active}
                            className={className}
                            disabled={state.locked}
                            onClick={() => setMode(option)}
                        >
                            <span className="mcc-modal-mode-title">{killModeTitle(option)}</span>
                            <span className="mcc-modal-mode-signal">{killModeSignalText(option)}</span>
                        </button>
                    );
                })}
            </div>
        </ConfirmModal>
    );
}
