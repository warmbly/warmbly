// Pure helpers the Sending domains page and the import wizards share.
import type { RecordRow } from "./parts";
import type { SendingDomain } from "@/lib/api/models/app/emails/SendingDomain";
import { vendorLabel } from "@/lib/api/models/app/emails/MailboxSources";

/** The CNAME a tracking host needs. */
export function trackingRecord(host: string, target: string): RecordRow {
    return { key: "cname", type: "CNAME", name: host, value: target };
}

/** Why a typed website address cannot be a redirect target, read as the server reads it; null when it can. */
export function redirectTargetProblem(raw: string, domain: string): string | null {
    const t = raw.trim();
    if (!t) return "Enter the website to send visitors to.";
    let u: URL;
    try {
        u = new URL(t.includes("://") ? t : `https://${t}`);
    } catch {
        return "Enter a website address, like https://yourcompany.com.";
    }
    if ((u.protocol !== "https:" && u.protocol !== "http:") || !u.hostname.includes(".")) {
        return "Enter a website address, like https://yourcompany.com.";
    }
    const host = u.hostname.toLowerCase();
    if (host === domain || host === `www.${domain}`) return "The redirect cannot point back at the same domain.";
    return null;
}

/** A domain nobody can add DNS records to, so neither choice applies. */
export function noDnsControl(domain: string): boolean {
    return /\.onmicrosoft\.com$/i.test(domain);
}

/** "live": verified hosts cover every mailbox; "partial": verified but some mailboxes use none; "pending": a host waits for DNS. */
export type TrackingState = "live" | "partial" | "pending" | "none";

export function trackingState(d: SendingDomain): TrackingState {
    const hosts = d.tracking_domains ?? [];
    if (hosts.length === 0) return "none";
    if (hosts.some((t) => !t.verified)) return "pending";
    const covered = hosts.reduce((n, t) => n + t.mailboxes, 0);
    return covered >= d.mailboxes ? "live" : "partial";
}

/** "vendor": the vendor forwards the root; "live" / "pending": the DNS redirect. */
export type RedirectState = "live" | "vendor" | "pending" | "none";

export function redirectState(d: SendingDomain): RedirectState {
    if (d.redirect?.verified) return "live";
    if (d.vendor_domain?.forwarding) return "vendor";
    if (d.redirect) return "pending";
    return "none";
}

/** Authentication fails, or a tracking host or redirect is stuck waiting for DNS. */
export function needsAttention(d: SendingDomain): boolean {
    const t = trackingState(d);
    return d.auth_state === "failing" || t === "pending" || t === "partial" || redirectState(d) === "pending";
}

/** The vendor managing a domain: the account holding it, else the one its mailboxes came from. */
export function domainVendor(d: SendingDomain): string {
    return d.vendor_domain?.vendor || d.vendors?.[0] || "";
}

export function vendorCanCname(d: SendingDomain): boolean {
    const v = d.vendor_domain;
    return !!v && v.can_dns && (v.dns_types ?? []).some((t) => t.toUpperCase() === "CNAME");
}

/** The website a redirect or forwarding points at, without the scheme or trailing slash. */
export function shortUrl(url: string): string {
    return url.replace(/^https?:\/\//, "").replace(/\/$/, "");
}

export interface DomainStats {
    domains: number;
    mailboxes: number;
    authenticated: number;
    tracking: number;
    redirects: number;
    attention: number;
    vendor: number;
}

export function domainStats(list: SendingDomain[]): DomainStats {
    const s: DomainStats = { domains: 0, mailboxes: 0, authenticated: 0, tracking: 0, redirects: 0, attention: 0, vendor: 0 };
    for (const d of list) {
        s.domains++;
        s.mailboxes += d.mailboxes;
        if (d.auth_state === "passing") s.authenticated++;
        if (trackingState(d) === "live") s.tracking++;
        const r = redirectState(d);
        if (r === "live" || r === "vendor") s.redirects++;
        if (needsAttention(d)) s.attention++;
        if (domainVendor(d)) s.vendor++;
    }
    return s;
}

export type PillTone = "emerald" | "amber" | "rose" | "slate" | "sky";

export interface PillInfo {
    tone: PillTone;
    label: string;
    title: string;
}

export function authPill(d: SendingDomain): PillInfo {
    const found = [d.auth_spf && "SPF", d.auth_dkim && "DKIM", d.auth_dmarc && "DMARC"].filter(Boolean).join(", ");
    const detail = found ? `Found: ${found}.` : "No SPF, DKIM or DMARC record found yet.";
    if (d.auth_state === "passing") return { tone: "emerald", label: "Authenticated", title: detail };
    if (d.auth_state === "failing") return { tone: "rose", label: "Auth failing", title: `Authentication fails. ${detail}` };
    return { tone: "slate", label: "Not checked", title: "Authentication has not been checked yet." };
}

export function trackingPill(d: SendingDomain): PillInfo {
    const hosts = d.tracking_domains ?? [];
    const primary = hosts[0]?.host ?? "";
    switch (trackingState(d)) {
        case "live":
            return { tone: "emerald", label: primary, title: `Links and opens are tracked on ${primary}.` };
        case "partial": {
            const covered = hosts.reduce((n, t) => n + t.mailboxes, 0);
            return {
                tone: "amber",
                label: `${covered} of ${d.mailboxes} mailboxes`,
                title: `Only ${covered} of the ${d.mailboxes} mailboxes on ${d.domain} use a tracking domain.`,
            };
        }
        case "pending":
            return { tone: "amber", label: "Waiting for DNS", title: `${primary || "The tracking host"} does not resolve to Warmbly yet.` };
        default:
            return { tone: "slate", label: "Shared host", title: "Links use the shared tracking host." };
    }
}

export function redirectPill(d: SendingDomain): PillInfo {
    switch (redirectState(d)) {
        case "live":
            return { tone: "emerald", label: shortUrl(d.redirect!.target_url), title: `${d.domain} redirects to ${d.redirect!.target_url}.` };
        case "vendor": {
            const vendor = vendorLabel(d.vendor_domain!.vendor);
            return {
                tone: "emerald",
                label: shortUrl(d.vendor_domain!.forwarding!),
                title: `${vendor} forwards ${d.domain} to ${d.vendor_domain!.forwarding}.`,
            };
        }
        case "pending":
            return { tone: "amber", label: "Waiting for DNS", title: "The redirect's DNS records are not all in place yet." };
        default:
            return { tone: "slate", label: "None", title: `${d.domain} does not redirect anywhere yet.` };
    }
}

/** The website most of the workspace's domains already send visitors to, to offer for the rest. */
export function commonWebsite(list: SendingDomain[]): string {
    const counts = new Map<string, number>();
    for (const d of list) {
        const url = d.redirect?.target_url || d.vendor_domain?.forwarding || "";
        if (url) counts.set(url, (counts.get(url) ?? 0) + 1);
    }
    let best = "";
    let n = 0;
    for (const [url, c] of counts) {
        if (c > n) [best, n] = [url, c];
    }
    return best;
}

/** Why a typed tracking host cannot be used for a domain, or null. */
export function trackingHostProblem(raw: string, domain: string): string | null {
    const h = raw.trim().toLowerCase().replace(/\.$/, "");
    if (!h) return "Enter a tracking host, or untick tracking.";
    if (!/^[a-z0-9]([a-z0-9-]*[a-z0-9])?(\.[a-z0-9]([a-z0-9-]*[a-z0-9])?)+$/.test(h)) return `Use a plain hostname, like link.${domain}.`;
    if (!h.endsWith(`.${domain}`)) return `Use a subdomain of ${domain}, like link.${domain}.`;
    return null;
}
