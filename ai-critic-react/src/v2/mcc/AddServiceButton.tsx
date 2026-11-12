import { PlusIcon } from '../../pure-view/icons/PlusIcon';

export interface AddServiceButtonProps {
    onClick: () => void;
}

/**
 * AddServiceButton opens the add/edit service dialog. The dialog itself lives
 * in ServiceFormModal; this button stays in the list and never grows into a
 * form, so the page layout does not move when it is pressed.
 */
export function AddServiceButton({ onClick }: AddServiceButtonProps) {
    return (
        <button className="mcc-add-port-btn" onClick={onClick} type="button">
            <PlusIcon />
            <span>Add Service</span>
        </button>
    );
}
