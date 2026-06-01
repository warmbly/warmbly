// The Warmbly geometric mark. Single-path, inherits color via currentColor so
// it works on light or dark surfaces. Mirrors the wordmark used on the
// marketing site and dashboard so the admin app shares one brand.

export function Logo({ className }: { className?: string }) {
    return (
        <svg
            className={className}
            viewBox="0 0 746 764"
            fill="none"
            xmlns="http://www.w3.org/2000/svg"
            aria-hidden="true"
        >
            <path
                d="M222.805 644.772L186.274 108.881L704.5 451.158L484.5 451.158L245.5 196.158L444 463.5L222.805 644.772Z"
                fill="currentColor"
            />
        </svg>
    );
}
