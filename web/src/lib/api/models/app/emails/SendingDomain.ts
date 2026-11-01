// /emails/domains: every domain the workspace sends from, with its custom
// tracking host and the optional redirect of its root to the company website.
import type TrackingDomain from "./TrackingDomain";

export type DomainAuthState = "passing" | "failing" | "unknown";

/** One custom tracking host the domain's mailboxes use. */
export interface TrackingDomainUse {
    host: string;
    verified: boolean;
    mailboxes: number;
}

export type DNSRecordPurpose = "ownership" | "root" | "www" | "website";

/** One record to add at the DNS provider, and whether it is in place. */
export interface DNSRecord {
    purpose: DNSRecordPurpose;
    type: string;
    name: string;
    value: string;
    ok: boolean;
    /** Improves the result but is not needed to verify. */
    optional?: boolean;
}

/** Who answers visitors: this instance's tracking service, or Warmbly Cloud for a linked self-hosted instance. */
export type RedirectServer = "instance" | "cloud";

/** "ok": visitors get the redirect; "not_reaching": something else answers; "https_error": https fails; "unreachable": nothing answered this server's check. */
export type RedirectReachStatus = "ok" | "not_reaching" | "https_error" | "unreachable";

/** The likely cause, which picks the fix to show. */
export type RedirectReachHint = "" | "not_routed" | "host_header" | "wrong_target" | "certificate" | "no_listener" | "settling";

/** The last time the domain was opened the way a visitor would, after DNS verified. */
export interface RedirectReach {
    status: RedirectReachStatus;
    hint?: RedirectReachHint;
    detail?: string;
    /** The web server that answered in Warmbly's place, when it could be told: traefik, nginx, caddy, ... */
    proxy?: string;
    checked_at?: Date | null;
}

export interface DomainRedirect {
    id: string;
    domain: string;
    target_url: string;
    include_www: boolean;
    verified: boolean;
    verified_at?: Date | null;
    last_checked_at?: Date | null;
    last_error?: string;
    created_at: Date;
    records: DNSRecord[];
    served_by: RedirectServer;
    /** The tracking host that answers, which a proxy in front routes the domain to. */
    serve_host?: string;
    reach?: RedirectReach | null;
    /** False when target_url is not on one of the workspace's own domains, so visitors get the domain's default page. */
    target_active?: boolean;
    /** Why the saved destination is not in use, when target_active is false. */
    target_inactive_reason?: string;
    /** Set when the destination is the workspace's only once its website carries the workspace's TXT record. */
    website?: RedirectWebsite | null;
}

/** A destination website and the TXT record, at the website's own DNS, that proves it is the workspace's. */
export interface RedirectWebsite {
    domain: string;
    verified: boolean;
    /** Absent when the website is already one of the workspace's domains. */
    record?: DNSRecord | null;
}

export interface SendingDomain {
    domain: string;
    mailboxes: number;
    mail_hosts: string[];
    auth_state: DomainAuthState;
    auth_spf: boolean;
    auth_dkim: boolean;
    auth_dmarc: boolean;
    tracking_domains: TrackingDomainUse[];
    redirect?: DomainRedirect | null;
    /** Inbox vendors this domain's mailboxes were imported from. */
    vendors?: string[];
    vendor_connection_ids?: string[];
    /** Set when a connected vendor account holds the domain. */
    vendor_domain?: VendorDomainLink | null;
}

/** A sending domain an inbox vendor account holds, and what its API can do for it. */
export interface VendorDomainLink {
    vendor: string;
    connection_id: string;
    /** Where the vendor forwards the bare domain today, if anywhere. */
    forwarding?: string;
    can_forward: boolean;
    /** An empty target removes the forwarding. */
    can_unforward: boolean;
    /** The vendor's staff apply a forwarding change by hand, later. */
    forwarding_reviewed: boolean;
    can_dns: boolean;
    dns_types: string[];
}

/**
 * The tracking host to offer a domain: "active" is already in use and verified,
 * "found" already points at this instance, "suggested" still needs its CNAME.
 */
export interface TrackingSuggestion {
    host: string;
    status: "active" | "found" | "suggested";
    cname_target: string;
    /** Set when a connected inbox vendor account holds the domain. */
    vendor_domain?: VendorDomainLink | null;
}

export interface SetDomainTrackingResult {
    tracking: TrackingDomain;
    /** Mailboxes the host was put on. */
    mailboxes: number;
}

export interface SetDomainRedirectRequest {
    target_url: string;
    include_www?: boolean;
    /** Omitted keeps where the redirect is served from today. */
    served_by?: RedirectServer;
}

/** POST /emails/domains/bulk: one tracking subdomain and one redirect website for up to 100 domains. */
export interface BulkDomainSetupRequest {
    domains: string[];
    /** Sets <label>.<domain> as each domain's tracking host. */
    tracking_label?: string;
    redirect_url?: string;
    /** A domain's own tracking host or website, in place of the shared one. */
    tracking_hosts?: Record<string, string>;
    redirect_urls?: Record<string, string>;
    /** Where a redirect no vendor forwards is served from. */
    served_by?: RedirectServer;
}

/** "vendor": the vendor holding the domain did it; "dns": the record is yours to add; "cloud": Warmbly Cloud serves it, once its records are added. */
export type BulkVia = "vendor" | "dns" | "cloud";

export interface BulkDomainResult {
    domain: string;
    tracking?: {
        host: string;
        via: BulkVia;
        verified: boolean;
        mailboxes: number;
        cname_target?: string;
        /** Why the vendor did not write the record when it was asked. */
        note?: string;
        error?: string;
        code?: string;
    };
    redirect?: {
        target_url?: string;
        via: BulkVia;
        verified: boolean;
        /** The vendor's staff apply the forwarding later. */
        reviewed?: boolean;
        error?: string;
        code?: string;
    };
}

/** Domains per bulk request: the server takes 100, but at 20s a domain, 4 at a time, 25 always answers inside the client timeout. */
export const BULK_DOMAINS_CHUNK = 25;
