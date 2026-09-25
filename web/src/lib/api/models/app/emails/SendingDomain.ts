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

export type DNSRecordPurpose = "ownership" | "root" | "www";

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
}

/** "vendor": the vendor holding the domain did it; "dns": the record is yours to add. */
export type BulkVia = "vendor" | "dns";

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

/** The most bulk setup takes in one request. */
export const BULK_DOMAINS_MAX = 100;
