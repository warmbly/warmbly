import { RiLinkedinBoxFill } from "@remixicon/react";
import { cn } from "@/lib/utils";

// LinkedIn's own mark, in its blue unless a className sets the colour.
export default function LinkedInMark({ className }: { className?: string }) {
    return <RiLinkedinBoxFill aria-hidden className={cn("shrink-0 text-[#0a66c2]", className)} />;
}

/** A small icon link to a LinkedIn profile, or nothing without a URL. */
export function LinkedInLink({ url, label, className }: { url: string | null | undefined; label: string; className?: string }) {
    const href = safeLinkedInUrl(url);
    if (!href) return null;
    return (
        <a
            href={href}
            target="_blank"
            rel="noopener noreferrer nofollow"
            aria-label={`${label} on LinkedIn`}
            title="Open on LinkedIn"
            onClick={(e) => e.stopPropagation()}
            onMouseDown={(e) => e.stopPropagation()}
            className={cn("inline-flex items-center justify-center rounded hover:opacity-80", className)}
        >
            <LinkedInMark className="size-3.5" />
        </a>
    );
}

/** The URL as an https link on linkedin.com, or null for anything else. */
export function safeLinkedInUrl(raw: string | null | undefined): string | null {
    const v = (raw ?? "").trim();
    if (!v) return null;
    try {
        const u = new URL(/^https?:\/\//i.test(v) ? v : `https://${v}`);
        if (u.protocol !== "https:" && u.protocol !== "http:") return null;
        const host = u.hostname.toLowerCase();
        if (host !== "linkedin.com" && !host.endsWith(".linkedin.com")) return null;
        u.protocol = "https:";
        return u.toString();
    } catch {
        return null;
    }
}
