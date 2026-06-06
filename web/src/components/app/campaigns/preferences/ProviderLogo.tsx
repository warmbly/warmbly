// ProviderLogo — provider brand marks for the matching visual. Reuses the real,
// official Google + Outlook SVGs the app already ships (the same ones the Add
// Account flow uses, @/components/svg), so the logos are genuine and consistent
// rather than hand-drawn. SMTP/IMAP and unknown domains get a neutral mail mark.

import { Google, Outlook } from "@/components/svg";

export default function ProviderLogo({
    provider,
    className = "size-6",
    muted = false,
}: {
    provider: string;
    className?: string;
    muted?: boolean;
}) {
    const wrap = `inline-flex items-center justify-center shrink-0 ${muted ? "opacity-40 grayscale" : ""}`;

    if (provider === "gmail") {
        return (
            <span className={wrap}>
                <Google className={className} />
            </span>
        );
    }

    if (provider === "outlook") {
        return (
            <span className={wrap}>
                <Outlook className={className} />
            </span>
        );
    }

    // smtp_imap / other / unknown — neutral mail mark.
    return (
        <span className={wrap}>
            <svg
                viewBox="0 0 24 24"
                className={className}
                fill="none"
                stroke="#64748b"
                strokeWidth="2"
                strokeLinecap="round"
                strokeLinejoin="round"
                aria-hidden="true"
            >
                <rect x="3" y="5" width="18" height="14" rx="2" />
                <path d="m3 7.5 9 6 9-6" />
            </svg>
        </span>
    );
}
