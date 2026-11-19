// A domain the customer already owns, connected to Warmbly: what to show while its DNS moves over.
import type { DnsRecord, StoreDomain } from "@/lib/api/models/app/store/Store";
import type { OwnedDomain } from "./cart";

/** What a typed domain is sent as: lowercase, no scheme, path or leading www. */
export function normalizeDomainInput(raw: string): string {
    return raw
        .trim()
        .toLowerCase()
        .replace(/^https?:\/\//, "")
        .replace(/^www\./, "")
        .replace(/[/?#].*$/, "")
        .replace(/\.$/, "");
}

/** A light check before asking the server: two or more labels of letters, digits and dashes. */
export function looksLikeDomain(d: string): boolean {
    return /^(?=.{3,253}$)([a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?\.)+[a-z][a-z0-9-]{0,62}$/.test(d);
}

/** Mailboxes can be bought for it: purchased and active, or connected, active and with nothing in the way. */
export function readyForMailboxes(d: StoreDomain): boolean {
    if (d.source === "connected") return d.connection?.status === "active" && (d.dns_conflicts ?? []).length === 0;
    return d.status === "active";
}

/** The cart's view of a domain the workspace already has. */
export function ownedFromDomain(d: StoreDomain): OwnedDomain {
    return { domain: d.name, provider: d.mailbox_count > 0 ? d.provider : null, hasAdmin: d.mailbox_count > 0 };
}

/** Records to add at the DNS provider, the ownership record first. */
export function manualRecords(dns: DnsRecord[] | undefined): DnsRecord[] {
    const rank = (r: DnsRecord) => (r.purpose === "ownership" ? 0 : r.purpose === "verification" ? 1 : 2);
    return [...(dns ?? [])].sort((a, b) => rank(a) - rank(b));
}

/** "47 h 12 min left", "12 min left", or null once it has passed or when there is no deadline. */
export function timeLeft(expiresAt: Date | string | null | undefined, now = Date.now()): string | null {
    if (!expiresAt) return null;
    const t = (expiresAt instanceof Date ? expiresAt : new Date(expiresAt)).getTime();
    if (Number.isNaN(t)) return null;
    const mins = Math.floor((t - now) / 60_000);
    if (mins <= 0) return null;
    const h = Math.floor(mins / 60);
    const m = mins % 60;
    return h > 0 ? `${h} h ${m} min left` : `${m} min left`;
}

/** The host a record name stands for, so it can be pasted into a provider that wants the full name. */
export function fullRecordName(name: string, domain: string): string {
    if (!name || name === "@") return domain;
    return name.endsWith(`.${domain}`) || name === domain ? name : `${name}.${domain}`;
}
