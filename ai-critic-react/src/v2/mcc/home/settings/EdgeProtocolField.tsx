import { useState } from 'react';
import { TunnelProtocols } from '../../../../api/domains';
import type { TunnelProtocolInfo } from '../../../../api/domains';
import { FormField, FormSelect } from '../../../../pure-view/form';
import { Button } from '../../../../pure-view/buttons/Button';
import '../../../../pure-view/form/FormInput.css';
import './EdgeProtocolField.css';

export interface EdgeProtocolFieldProps {
    info: TunnelProtocolInfo | null;
    /** True while a save is in flight. */
    saving: boolean;
    /** Persist the chosen protocol; restart decides whether to bounce the connector. */
    onSave: (protocol: string, restart: boolean) => void;
    onError: (message: string) => void;
}

/**
 * Edge transport selector for the tunnel. http2 stays the default, so an
 * install that never touches this keeps the previous behaviour byte-for-byte.
 */
export function EdgeProtocolField({ info, saving, onSave, onError }: EdgeProtocolFieldProps) {
    const configured = info?.protocol || '';
    const effective = info?.effective || TunnelProtocols.HTTP2;
    const [selected, setSelected] = useState(configured || effective);

    const dirty = selected !== (configured || effective);
    const isDefault = configured === '';

    const handleSave = (restart: boolean) => {
        if (!selected) {
            onError('Select an edge protocol first.');
            return;
        }
        onSave(selected, restart);
    };

    return (
        <div className="edge-protocol">
            <FormField label="Edge Protocol">
                <FormSelect value={selected} onChange={setSelected} disabled={saving}>
                    <option value={TunnelProtocols.HTTP2}>
                        http2 — HTTP/2 over TCP (default)
                    </option>
                    <option value={TunnelProtocols.QUIC}>
                        quic — QUIC over UDP (better on lossy links)
                    </option>
                </FormSelect>
                <div className="edge-protocol-current">
                    active transport: <b>{effective}</b>
                    {isDefault && <span className="edge-protocol-badge">default</span>}
                </div>
            </FormField>

            <div className="edge-protocol-hint">
                <b>http2</b> multiplexes every stream over one TCP connection: on a lossy uplink a
                single stalled socket costs seconds per request. <b>quic</b> keeps streams
                independent and needs UDP/7844 to the edge.
            </div>

            <div className="edge-protocol-actions">
                <Button variant="primary" disabled={saving || !dirty} onClick={() => handleSave(true)}>
                    {saving ? 'Saving…' : 'Save & restart tunnel'}
                </Button>
                <Button
                    variant="secondary"
                    disabled={saving || !dirty}
                    onClick={() => handleSave(false)}
                    title="Persist only; the next tunnel start uses the new transport"
                >
                    Save only
                </Button>
                {dirty && (
                    <Button variant="cancel" disabled={saving} onClick={() => setSelected(configured || effective)}>
                        Reset
                    </Button>
                )}
            </div>
        </div>
    );
}
