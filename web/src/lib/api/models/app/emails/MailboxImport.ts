// Server-side mailbox import: POST /emails/imports/preview reads a file or a
// pasted list and says what it would do, POST /emails/imports starts a job the
// backend runs row by row, and the job's rows carry every outcome.
import type MailboxAllowance from "./MailboxAllowance";
import type { DomainRedirect, TrackingSuggestion } from "./SendingDomain";
import type { MailSecurity } from "./Service";

/** Which mailbox host an address belongs to; "" until detected. */
export type MailHost =
    | ""
    | "google_workspace"
    | "gmail"
    | "microsoft365"
    | "outlook"
    | "zoho"
    | "yahoo"
    | "aol"
    | "icloud"
    | "fastmail"
    | "godaddy"
    | "namecheap"
    | "ionos"
    | "hostinger"
    | "ovh"
    | "migadu"
    | "purelymail"
    | "rackspace"
    | "yandex"
    | "gmx"
    | "proton"
    | "other";

export type MailAuthMethod = "" | "password" | "app_password" | "oauth" | "delegated";

/** How a password signs in on a host. */
export type PasswordAuth = "app_password" | "password" | "oauth_only" | "unsupported";

/** A column the import can read. "ignore" leaves the column out. */
export type ImportField =
    | "email"
    | "name"
    | "first_name"
    | "last_name"
    | "password"
    | "app_password"
    | "smtp_password"
    | "imap_password"
    | "username"
    | "smtp_username"
    | "imap_username"
    | "smtp_host"
    | "smtp_port"
    | "smtp_security"
    | "imap_host"
    | "imap_port"
    | "imap_security"
    | "daily_limit"
    | "min_wait"
    | "warmup"
    | "warmup_start"
    | "warmup_max"
    | "warmup_increase"
    | "warmup_reply_rate"
    | "reply_to"
    | "signature"
    | "tags"
    | "timezone"
    | "ignore";

/** Column index (as a string) to the field it maps to. */
export type ImportMapping = Record<string, ImportField>;

export type OnExisting = "update" | "skip";

/** Batch defaults applied where a row does not carry its own value. */
export interface MailboxImportSettings {
    tag_ids?: string[];
    daily_limit?: number;
    /** Seconds between sends. */
    min_wait?: number;
    warmup?: boolean;
    warmup_start?: number;
    warmup_max?: number;
    warmup_increase?: number;
    warmup_reply_rate?: number;
    reply_to?: string;
    signature?: string;
    timezone?: string;
}

export interface MailboxImportOptions {
    /** null lets the server decide. */
    has_header?: boolean | null;
    shared_password?: string;
    on_existing?: OnExisting;
    /** Create only; defaults to true on the server. */
    save_mapping?: boolean;
    /** Create only; preview ignores it. */
    settings?: MailboxImportSettings;
    /** A tracking host per sending domain, e.g. {"acme.io": "track.acme.io"}, applied as each mailbox connects. */
    tracking_domains?: Record<string, string>;
    /** A website per sending domain its root redirects to, e.g. {"acme.io": "https://acme.com"}. */
    redirects?: Record<string, string>;
}

/** A file or a pasted list, plus what the user decided about it. */
export interface MailboxImportInput {
    file?: File;
    text?: string;
    mapping?: ImportMapping;
    options?: MailboxImportOptions;
}

export type ColumnSource = "saved" | "vendor" | "alias" | "shape" | "jev" | "none";

export interface ImportColumn {
    index: number;
    header: string;
    /** Masked by the server when the column is secret. */
    samples: string[];
    secret: boolean;
    field: ImportField;
    source: ColumnSource;
    confidence: number;
}

export type PreviewRowStatus = "ready" | "needs_signin" | "invalid" | "existing" | "duplicate";

export interface ImportLeg {
    host: string;
    port: number;
    security: MailSecurity | "";
    username?: string;
}

export interface PreviewRow {
    line: number;
    email: string;
    name: string;
    mail_host: MailHost;
    auth_method: MailAuthMethod;
    status: PreviewRowStatus;
    cause?: string;
    problem?: string;
    smtp?: ImportLeg;
    imap?: ImportLeg;
}

export type DetectionSource = "known" | "mx" | "autoconfig" | "ispdb" | "srv" | "file" | "none";

export interface DomainDetection {
    domain: string;
    mail_host: MailHost;
    label: string;
    source: DetectionSource;
    rows: number;
    password_auth: PasswordAuth;
    app_password_url?: string;
    smtp?: ImportLeg;
    imap?: ImportLeg;
    auth?: { state: string; spf: boolean; dkim: boolean; dmarc: boolean };
    /** The tracking host this domain's mailboxes can use; absent when tracking is off on the instance. */
    tracking?: TrackingSuggestion | null;
    /** The domain's root redirect, when one exists. */
    redirect?: DomainRedirect | null;
}

export interface ImportIssue {
    cause: string;
    title: string;
    fix: string;
    count: number;
    lines: number[];
}

export interface MailboxImportSummary {
    total: number;
    ready: number;
    needs_signin: number;
    invalid: number;
    existing: number;
    duplicate: number;
}

export interface MailboxImportPreview {
    format: "csv" | "xlsx" | "paste";
    has_header: boolean;
    vendor: { id: string; label: string } | null;
    columns: ImportColumn[];
    mapping: ImportMapping;
    saved_mapping: boolean;
    summary: MailboxImportSummary;
    /** The first 100 rows. */
    rows: PreviewRow[];
    domains: DomainDetection[];
    issues: ImportIssue[];
    allowance?: MailboxAllowance;
    max_rows: number;
}

export type MailboxImportStatus = "running" | "completed" | "cancelled";

export type ImportRowStatus =
    | "queued"
    | "running"
    | "connected"
    | "updated"
    | "skipped"
    | "failed"
    | "needs_signin"
    | "cancelled";

export type MailboxImportCounts = Record<ImportRowStatus, number>;

export interface ImportCause {
    cause: string;
    title: string;
    fix: string;
    count: number;
    retryable: boolean;
}

/** Where an import's rows came from: a file, a pasted list, a vendor's API, or an admin grant. */
export type MailboxImportSource = "file" | "paste" | "vendor" | "workspace";

export interface MailboxImport {
    id: string;
    status: MailboxImportStatus;
    source: MailboxImportSource;
    /** The file's name; for a vendor or grant import, the account's label or the grant's domains. */
    filename: string;
    /** The detected export vendor, the vendor id, or "google" / "microsoft" for a grant. */
    vendor: string;
    on_existing: OnExisting;
    created_at: Date;
    updated_at: Date;
    finished_at?: Date | null;
    credentials_expire_at?: Date | null;
    total: number;
    counts: MailboxImportCounts;
    causes: ImportCause[];
    /** Rows an inbox vendor is still authorizing, by provider; older servers do not send it. */
    authorizing?: Partial<Record<"google" | "microsoft", number>>;
}

export interface ImportRow {
    line: number;
    email: string;
    name: string;
    mail_host: MailHost;
    status: ImportRowStatus;
    code: string;
    cause: string;
    message: string;
    email_account_id?: string;
    retryable: boolean;
    /** The row as uploaded, minus secrets. */
    fields: Record<string, string>;
    updated_at: Date;
}

export interface RowFixLeg {
    host?: string;
    port?: number;
    security?: MailSecurity;
    username?: string;
    password?: string;
}

export interface RowFix {
    password?: string;
    app_password?: string;
    username?: string;
    smtp?: RowFixLeg;
    imap?: RowFixLeg;
}

export interface RetryImportRequest {
    cause?: string;
    lines?: number[];
    password?: string;
}

export interface ImportPagination {
    next_cursor: string | null;
    has_more: boolean;
}

export interface MailboxImportList {
    data: MailboxImport[];
    pagination: ImportPagination;
}

export interface ImportRowList {
    data: ImportRow[];
    pagination: ImportPagination;
}

export interface ImportRowsParams {
    status?: ImportRowStatus | "";
    cause?: string;
    cursor?: string;
    limit?: number;
}

/** Rows that have reached an outcome. */
export function importDone(c: MailboxImportCounts): number {
    return c.connected + c.updated + c.skipped + c.failed + c.needs_signin + c.cancelled;
}
