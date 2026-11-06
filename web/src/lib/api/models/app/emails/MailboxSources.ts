// Mailbox sources other than a file: an inbox vendor's API (/emails/vendors)
// and an administrator's grant over a Google Workspace domain or a Microsoft
// 365 tenant (/emails/grants). Both start a MailboxImport the backend works in
// the background, so their result screen is the file import's.
import type { MailboxImportOptions } from "./MailboxImport";

export interface VendorField {
    key: string;
    label: string;
    secret: boolean;
    required: boolean;
    help?: string;
}

export interface VendorCatalogEntry {
    id: string;
    label: string;
    website: string;
    key_help_url: string;
    fields: VendorField[];
    /** The vendor can sign in again as its Google mailboxes, so they reconnect in place. */
    signs_in?: boolean;
}

export type SourceStatus = "active" | "invalid";

/** A vendor account the workspace imports from. Its key never leaves the server. */
export interface VendorConnection {
    id: string;
    vendor: string;
    label: string;
    status: SourceStatus;
    last_error?: string;
    last_used_at?: Date | null;
    created_at: Date;
    /** Mailboxes connected through it. */
    mailboxes: number;
}

export type VendorMailboxProvider = "google" | "microsoft" | "smtp" | "";

export interface VendorMailbox {
    id: string;
    email: string;
    name: string;
    domain: string;
    provider: VendorMailboxProvider;
    /** The vendor's own status for the mailbox, as it reports it. */
    status: string;
    /** The vendor workspace or organization holding it, where the vendor has them. */
    workspace?: string;
    connected: boolean;
    email_account_id?: string;
}

export interface CreateVendorRequest {
    vendor: string;
    label: string;
    fields: Record<string, string>;
}

export interface UpdateVendorRequest {
    label?: string;
    /** Replaces the key, after the vendor accepts it. */
    fields?: Record<string, string>;
}

/** Options for a vendor or grant import: no file, so no mapping or header. */
export type SourceImportOptions = Required<Pick<MailboxImportOptions, "on_existing" | "settings">> &
    Pick<MailboxImportOptions, "tracking_domains" | "redirects">;

export interface VendorImportRequest {
    mailbox_ids?: string[];
    /** Every mailbox not connected yet. */
    all?: boolean;
    options: SourceImportOptions;
}

export type GrantProvider = "google" | "microsoft";

export interface GrantConfig {
    google_enabled: boolean;
    google_client_id?: string;
    google_scopes: string[];
    microsoft_enabled: boolean;
    /** Settings an operator still has to set; empty when enabled. */
    google_missing?: string[];
    microsoft_missing?: string[];
    /** The per-mailbox Google sign-in client a Workspace admin marks Trusted; absent with no client configured. */
    google_mailbox_client_id?: string;
}

export interface DomainGrant {
    id: string;
    provider: GrantProvider;
    /** The Google domain, or the Microsoft tenant id. */
    tenant: string;
    admin_email?: string;
    domains: string[];
    status: SourceStatus;
    last_error?: string;
    verified_at?: Date | null;
    created_at: Date;
    mailboxes: number;
}

/** One account in a granted domain or tenant. */
export interface DirectoryUser {
    /** The address (Google) or the Graph user id (Microsoft). */
    id: string;
    email: string;
    name: string;
    /** False for a suspended or disabled account. */
    enabled: boolean;
    connected: boolean;
    email_account_id?: string;
    /** Already a mailbox here on per-mailbox sign-in; connecting moves it onto the grant, keeping its history. */
    upgrade?: boolean;
}

export interface GrantConnectRequest {
    user_ids?: string[];
    /** Every enabled user not connected yet. */
    all?: boolean;
    options: SourceImportOptions;
}

/** A mailbox on the older per-mailbox Google sign-in, without full mail access. */
export interface MigrationMailbox {
    id: string;
    email: string;
    name: string;
    status: string;
    /** The healthy vendor connection that can reconnect it in place, when one can. */
    vendor_connection_id?: string;
    vendor?: string;
}

/** Where one mailbox's vendor reconnect stands. */
export type ReconnectStatus = "queued" | "trusting" | "signing_in" | "connected" | "failed";

export interface VendorReconnectMailbox {
    email_account_id: string;
    email: string;
    status: ReconnectStatus;
    message?: string;
}

export type SigninMethod = "google_signin" | "google_grant" | "app_password" | "other";

/** How a mailbox signs in and the switches that can succeed for it (GET /emails/:id/signin). */
export interface MailboxSignin {
    method: SigninMethod;
    /** Set for a Google sign-in whose granted permissions are recorded. */
    full_mail_access: boolean | null;
    personal: boolean;
    managed: boolean;
    options: {
        google_signin: { client_id: string } | null;
        app_password: boolean;
        vendor: { connection_id: string; vendor: string; label: string } | null;
        grant: { id: string; domain: string } | null;
    };
    /** Warmbly reconnects it through its vendor without a click. */
    auto_switch: boolean;
    switch: { status: ReconnectStatus; message?: string; automatic: boolean; at: string } | null;
}

/** A reconnect that has not ended yet. */
export function reconnectOpen(status?: ReconnectStatus | null): boolean {
    return status === "queued" || status === "trusting" || status === "signing_in";
}

/**
 * One domain's mailboxes on per-mailbox Google sign-in. "personal" is a shared
 * address like gmail.com (moves to an app password); "workspace" moves to an
 * admin grant, which `grant_id` names when the domain already has one.
 */
export interface SigninMigration {
    domain: string;
    kind: "workspace" | "personal";
    grant_id?: string;
    mailboxes: MigrationMailbox[];
}

export interface SigninMigrationList {
    data: SigninMigration[];
    total: number;
}

export interface ListResponse<T> {
    data: T[];
}

// Vendor ids the backend knows, for labels where the catalog is not loaded.
const VENDOR_LABELS: Record<string, string> = {
    inboxkit: "InboxKit",
    zapmail: "Zapmail",
    mailforge: "Mailforge",
    infraforge: "Infraforge",
    maildoso: "Maildoso",
    cheapinboxes: "Cheap Inboxes",
    scaledmail: "ScaledMail",
};

export const GRANT_PROVIDER_LABELS: Record<GrantProvider, string> = {
    google: "Google Workspace",
    microsoft: "Microsoft 365",
};

export function vendorLabel(id?: string | null): string {
    if (!id) return "";
    return VENDOR_LABELS[id] ?? GRANT_PROVIDER_LABELS[id as GrantProvider] ?? id;
}

/** The domain part of an address, lower-cased. */
export function domainOf(email: string): string {
    const at = email.lastIndexOf("@");
    return at >= 0 ? email.slice(at + 1).toLowerCase() : "";
}

/** The grant that covers an address, if any. */
export function grantFor(grants: DomainGrant[] | undefined, email: string): DomainGrant | undefined {
    const d = domainOf(email);
    if (!d || !grants) return undefined;
    return grants.find((g) => g.tenant.toLowerCase() === d || g.domains.some((x) => x.toLowerCase() === d));
}

/** A grant as people know it: the Google domain, or the Microsoft tenant's domains. */
export function grantName(g: DomainGrant): string {
    if (g.provider === "google") return g.tenant;
    return g.domains.length > 0 ? g.domains.join(", ") : g.tenant;
}
