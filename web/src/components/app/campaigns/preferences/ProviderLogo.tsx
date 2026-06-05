// ProviderLogo — small app-icon tiles with the recognizable brand mark for each
// email provider we match against (Gmail, Outlook, and a neutral mark for
// SMTP/IMAP or any other domain). Brand colours are intentional inline hex —
// logos are exempt from the slate/sky chrome palette so they stay recognizable.

export default function ProviderLogo({
    provider,
    className = "size-6",
    muted = false,
}: {
    provider: string;
    className?: string;
    muted?: boolean;
}) {
    const tile = `inline-flex items-center justify-center rounded-md shrink-0 ${className} ${
        muted ? "opacity-40 grayscale" : ""
    }`;

    if (provider === "gmail") {
        return (
            <span className={`${tile} bg-white ring-1 ring-slate-200`}>
                <svg viewBox="0 0 48 48" className="w-[66%] h-[66%]" aria-hidden="true">
                    <path fill="#4caf50" d="M45 16.2l-5 2.75-5 4.75L35 40h7a3 3 0 0 0 3-3V16.2z" />
                    <path fill="#1e88e5" d="M3 16.2l3.614 1.71L13 23.7V40H6a3 3 0 0 1-3-3V16.2z" />
                    <polygon
                        fill="#e53935"
                        points="35,11.2 24,19.45 13,11.2 12,17 13,23.7 24,31.95 35,23.7 36,17"
                    />
                    <path fill="#c62828" d="M3 12.298V16.2l10 7.5V11.2L9.876 8.859A2.99 2.99 0 0 0 3 12.298z" />
                    <path fill="#fbc02d" d="M45 12.298V16.2l-10 7.5V11.2l3.124-2.341A2.99 2.99 0 0 1 45 12.298z" />
                </svg>
            </span>
        );
    }

    if (provider === "outlook") {
        return (
            <span className={`${tile} ring-1 ring-black/5`} style={{ backgroundColor: "#0F6CBD" }}>
                <svg viewBox="0 0 24 24" className="w-[64%] h-[64%]" aria-hidden="true">
                    {/* the signature Outlook "O" */}
                    <ellipse cx="9.2" cy="12" rx="4.6" ry="5.4" fill="none" stroke="#fff" strokeWidth="2.5" />
                    {/* envelope card */}
                    <path
                        fill="#fff"
                        d="M15 8.6h5.7c.35 0 .6.26.6.6v5.6c0 .34-.25.6-.6.6H15z"
                    />
                    <path fill="none" stroke="#0F6CBD" strokeWidth="1.2" d="M15.4 9.7l3 2 3-2" />
                </svg>
            </span>
        );
    }

    // smtp_imap / other / unknown — neutral mail mark.
    return (
        <span className={`${tile} bg-slate-500`}>
            <svg
                viewBox="0 0 24 24"
                fill="none"
                stroke="#fff"
                strokeWidth="2"
                strokeLinecap="round"
                strokeLinejoin="round"
                className="w-[56%] h-[56%]"
                aria-hidden="true"
            >
                <rect x="3" y="5" width="18" height="14" rx="2" />
                <path d="m3 7.5 9 6 9-6" />
            </svg>
        </span>
    );
}
