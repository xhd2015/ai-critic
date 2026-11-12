import { useEffect, useRef } from 'react';
import { createPortal } from 'react-dom';
import type { ServicePreset, ServiceStatus } from '../../api/services';
import type { ProviderInfo } from '../../hooks/usePortForwards';
import { ServiceFormFields } from './ServiceFormFields';
import type { ServiceFormState } from './serviceFormState';
import './KillProcessModal.css';
import './ServiceFormModal.css';

export interface ServiceFormModalProps {
    form: ServiceFormState;
    onChange: (updater: (prev: ServiceFormState) => ServiceFormState) => void;
    onSubmit: () => void;
    onClose: () => void;
    saving: boolean;
    /** Validation or save failure, shown inline inside the dialog. */
    error?: string | null;
    homeDir: string;
    providers: ProviderInfo[];
    presets: ServicePreset[];
    onApplyPreset: (preset: ServicePreset) => void;
    /** The service being edited, used to flag start-time settings that need a restart. */
    service?: ServiceStatus;
}

/**
 * ServiceFormModal hosts the add/edit service form in a portal dialog: a bottom
 * sheet on phones, a centred two-column dialog on desktop. It never renders
 * inline in the list, so opening it cannot push the page around.
 */
export function ServiceFormModal({
    form,
    onChange,
    onSubmit,
    onClose,
    saving,
    error,
    homeDir,
    providers,
    presets,
    onApplyPreset,
    service,
}: ServiceFormModalProps) {
    const dialogRef = useRef<HTMLFormElement | null>(null);
    // Escape and the scroll lock are installed once; refs keep the handlers
    // from re-subscribing (and re-locking the page) on every keystroke.
    const closeRef = useRef(onClose);
    const savingRef = useRef(saving);
    closeRef.current = onClose;
    savingRef.current = saving;

    useEffect(() => {
        const focusable = () => {
            const dialog = dialogRef.current;
            if (!dialog) return [] as HTMLElement[];
            const nodes = dialog.querySelectorAll<HTMLElement>(
                'button:not([disabled]), input:not([disabled]), textarea:not([disabled]), select:not([disabled]), a[href], [tabindex]:not([tabindex="-1"])',
            );
            return [...nodes].filter((node) => node.offsetParent !== null);
        };
        const onKeyDown = (event: KeyboardEvent) => {
            if (event.key === 'Escape') {
                if (savingRef.current) return;
                event.stopPropagation();
                closeRef.current();
                return;
            }
            if (event.key !== 'Tab') return;
            // Keep Tab inside the dialog: it sits above the page, so focusing
            // the list behind it would be invisible navigation.
            const items = focusable();
            if (items.length === 0) return;
            const first = items[0];
            const last = items[items.length - 1];
            const dialog = dialogRef.current;
            const current = document.activeElement;
            // The dialog itself holds focus on phones, so it counts as "outside"
            // any control: Tab enters at the first field, Shift+Tab at the last.
            const atEdge = (edge: HTMLElement) => current === edge || current === dialog || !dialog?.contains(current);
            if (event.shiftKey && atEdge(first)) {
                event.preventDefault();
                last.focus();
            } else if (!event.shiftKey && atEdge(last)) {
                event.preventDefault();
                first.focus();
            }
        };
        document.addEventListener('keydown', onKeyDown);
        return () => document.removeEventListener('keydown', onKeyDown);
    }, []);

    useEffect(() => {
        const previousOverflow = document.body.style.overflow;
        document.body.style.overflow = 'hidden';
        return () => {
            document.body.style.overflow = previousOverflow;
        };
    }, []);

    useEffect(() => {
        // Restore focus to the button that opened the dialog, so closing it does
        // not drop the keyboard user back to the top of the page.
        const previouslyFocused = document.activeElement instanceof HTMLElement ? document.activeElement : null;
        if (window.matchMedia('(min-width: 768px)').matches) {
            const input = dialogRef.current?.querySelector('input');
            if (input instanceof HTMLInputElement) input.focus();
        } else {
            // Phones focus the sheet itself: no on-screen keyboard, but focus
            // leaves the page behind the overlay so Esc and Tab still work.
            dialogRef.current?.focus();
        }
        return () => {
            if (previouslyFocused && document.contains(previouslyFocused)) previouslyFocused.focus();
        };
    }, []);

    const title = form.id ? 'Edit Service' : 'Add Service';

    const handleSubmit = (event: React.FormEvent<HTMLFormElement>) => {
        event.preventDefault();
        if (saving) return;
        onSubmit();
    };

    return createPortal(
        <div className="mcc-modal-overlay mcc-form-overlay">
            <form
                ref={dialogRef}
                className="mcc-modal mcc-modal--form"
                tabIndex={-1}
                role="dialog"
                aria-modal="true"
                aria-label={title}
                onSubmit={handleSubmit}
            >
                <div className="mcc-modal-header">
                    <h3>{title}</h3>
                    <button className="mcc-modal-close" onClick={onClose} type="button" disabled={saving}>×</button>
                </div>
                <div className="mcc-modal-body mcc-form-body">
                    <ServiceFormFields
                        form={form}
                        onChange={onChange}
                        homeDir={homeDir}
                        providers={providers}
                        presets={presets}
                        onApplyPreset={onApplyPreset}
                        service={service}
                    />
                    {error && <div className="mcc-modal-error">{error}</div>}
                </div>
                <div className="mcc-modal-footer">
                    <button className="mcc-modal-btn mcc-modal-btn-cancel" onClick={onClose} type="button" disabled={saving}>
                        Cancel
                    </button>
                    <button className="mcc-modal-btn mcc-modal-btn-primary" type="submit" disabled={saving}>
                        {saving ? 'Saving...' : form.id ? 'Update Service' : 'Save Service'}
                    </button>
                </div>
            </form>
        </div>,
        document.body,
    );
}
