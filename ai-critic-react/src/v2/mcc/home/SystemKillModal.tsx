import { useState } from 'react';
import { ConfirmModal } from '../ConfirmModal';
import { formatBytes, killHostProcess, type HostProcess } from '../../../api/processes';
import { KILL_MODES, killModeSignalText, killModeTitle, resolveKillModal, type KillMode } from './killMode';

export function SystemKillModal({
    proc,
    onClose,
    onKilled,
}: {
    proc: HostProcess;
    onClose: () => void;
    onKilled: () => void;
}) {
    const [mode, setMode] = useState<KillMode>('graceful');
    const state = resolveKillModal({
        pid: proc.pid,
        mode,
        protectedReason: proc.protected,
        isTunnel: (proc.tags ?? []).includes('tun'),
    });

    return (
        <ConfirmModal
            title="Kill Process"
            message={state.message}
            warning={state.warning}
            info={{
                Name: proc.name,
                PID: String(proc.pid),
                User: proc.user,
                'CPU / RSS': `${proc.cpu_percent.toFixed(1)}% · ${formatBytes(proc.rss_bytes)}`,
            }}
            command={state.command}
            confirmLabel={state.confirmLabel}
            confirmVariant={state.locked ? 'default' : 'danger'}
            onConfirm={async () => {
                if (state.locked) return;
                await killHostProcess(proc.pid, state.signal);
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
