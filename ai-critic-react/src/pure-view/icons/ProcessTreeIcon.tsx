export function ProcessTreeIcon({ className }: { className?: string }) {
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
            <rect x="9" y="2" width="6" height="5" rx="1" />
            <rect x="2" y="17" width="6" height="5" rx="1" />
            <rect x="16" y="17" width="6" height="5" rx="1" />
            <path d="M12 7v5" />
            <path d="M5 17v-3a2 2 0 0 1 2-2h10a2 2 0 0 1 2 2v3" />
        </svg>
    );
}
