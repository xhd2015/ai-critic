export function CheckboxIcon({ checked, className }: { checked?: boolean; className?: string }) {
    return (
        <svg
            xmlns="http://www.w3.org/2000/svg"
            width="15"
            height="15"
            viewBox="0 0 24 24"
            fill="none"
            stroke="currentColor"
            strokeWidth="2"
            strokeLinecap="round"
            strokeLinejoin="round"
            className={className}
            aria-hidden="true"
            focusable="false"
        >
            <rect x="4" y="4" width="16" height="16" rx="3" />
            {checked ? <path d="M8 12l3 3 5-6" /> : null}
        </svg>
    );
}
