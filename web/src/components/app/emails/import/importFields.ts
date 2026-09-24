// The import's field catalog: what each mapping target is called, which group
// it sits in, and the rules the wizard checks before it moves on.
import type {
    ColumnSource,
    ImportColumn,
    ImportField,
    ImportMapping,
    ImportRowStatus,
    MailboxImportPreview,
    PreviewRowStatus,
} from "@/lib/api/models/app/emails/MailboxImport";
import type { SelectOption } from "@/components/ui/select-menu";
import { vendorLabel } from "@/lib/api/models/app/emails/MailboxSources";

export const FIELD_GROUPS: { group: string; fields: { id: ImportField; label: string }[] }[] = [
    {
        group: "Identity",
        fields: [
            { id: "email", label: "Email address" },
            { id: "name", label: "Full name" },
            { id: "first_name", label: "First name" },
            { id: "last_name", label: "Last name" },
        ],
    },
    {
        group: "Sign-in",
        fields: [
            { id: "password", label: "Password" },
            { id: "app_password", label: "App password" },
            { id: "username", label: "Username (both)" },
            { id: "smtp_username", label: "SMTP username" },
            { id: "smtp_password", label: "SMTP password" },
            { id: "imap_username", label: "IMAP username" },
            { id: "imap_password", label: "IMAP password" },
        ],
    },
    {
        group: "Servers",
        fields: [
            { id: "smtp_host", label: "SMTP host" },
            { id: "smtp_port", label: "SMTP port" },
            { id: "smtp_security", label: "SMTP security" },
            { id: "imap_host", label: "IMAP host" },
            { id: "imap_port", label: "IMAP port" },
            { id: "imap_security", label: "IMAP security" },
        ],
    },
    {
        group: "Sending",
        fields: [
            { id: "daily_limit", label: "Daily limit" },
            { id: "min_wait", label: "Seconds between sends" },
            { id: "reply_to", label: "Reply-to" },
            { id: "signature", label: "Signature" },
            { id: "timezone", label: "Timezone" },
        ],
    },
    {
        group: "Warmup",
        fields: [
            { id: "warmup", label: "Warmup on/off" },
            { id: "warmup_start", label: "Warmup start" },
            { id: "warmup_max", label: "Warmup max" },
            { id: "warmup_increase", label: "Warmup daily increase" },
            { id: "warmup_reply_rate", label: "Warmup reply rate" },
        ],
    },
    { group: "Other", fields: [{ id: "tags", label: "Tags" }] },
    { group: "Ignore", fields: [{ id: "ignore", label: "Don't import" }] },
];

export const FIELD_OPTIONS: SelectOption[] = FIELD_GROUPS.flatMap((g) =>
    g.fields.map((f) => ({ value: f.id, label: f.label, group: g.group })),
);

export function fieldLabel(field: string): string {
    return FIELD_OPTIONS.find((o) => o.value === field)?.label ?? field;
}

export const PASSWORD_FIELDS: ImportField[] = ["password", "app_password", "smtp_password", "imap_password"];

/** Fields a column list may carry more than once; everything else is one column only. */
const REPEATABLE: ImportField[] = ["ignore", "tags"];

export function isRepeatable(field: ImportField): boolean {
    return REPEATABLE.includes(field);
}

export const SOURCE_CHIPS: Partial<Record<ColumnSource, { label: string; cls: string; hint: string }>> = {
    saved: { label: "Saved", cls: "bg-emerald-50 text-emerald-700", hint: "From the mapping you used last time for these headers" },
    vendor: { label: "", cls: "bg-sky-50 text-sky-700", hint: "The vendor's export format" },
    alias: { label: "Detected", cls: "bg-slate-100 text-slate-600", hint: "Recognised from the header" },
    shape: { label: "Suggested", cls: "bg-amber-50 text-amber-700", hint: "Guessed from what the values look like" },
    jev: { label: "Suggested", cls: "bg-amber-50 text-amber-700", hint: "Guessed from what the values look like" },
};

export function hasEmail(mapping: ImportMapping): boolean {
    return Object.values(mapping).includes("email");
}

export function hasPassword(mapping: ImportMapping): boolean {
    return Object.values(mapping).some((f) => PASSWORD_FIELDS.includes(f));
}

/** Every column came from a saved mapping or a known vendor export, so there is nothing to review. */
export function mappedAutomatically(p: MailboxImportPreview): boolean {
    if (p.columns.length === 0) return false;
    const certain = p.columns.every((c) => (c.source === "saved" || c.source === "vendor") && c.confidence >= 1);
    return certain && hasEmail(p.mapping) && hasPassword(p.mapping);
}

/** Why the mapping cannot be imported yet, or null. */
export function mappingProblem(mapping: ImportMapping, sharedPassword: string): string | null {
    if (!hasEmail(mapping)) return "Pick the column that holds the email address.";
    if (!hasPassword(mapping) && !sharedPassword) return "Pick a password column, or enter one password for every mailbox.";
    return null;
}

/** The mapping as columns report it, for a preview that returned no mapping object. */
export function mappingFromColumns(columns: ImportColumn[]): ImportMapping {
    const m: ImportMapping = {};
    for (const c of columns) m[String(c.index)] = c.field;
    return m;
}

export const PREVIEW_STATUS: Record<PreviewRowStatus, { label: string; cls: string }> = {
    ready: { label: "Ready", cls: "text-emerald-700 bg-emerald-50" },
    needs_signin: { label: "Needs sign-in", cls: "text-sky-700 bg-sky-50" },
    existing: { label: "Already here", cls: "text-slate-600 bg-slate-100" },
    duplicate: { label: "Duplicate", cls: "text-slate-600 bg-slate-100" },
    invalid: { label: "Invalid", cls: "text-red-700 bg-red-50" },
};

export const ROW_STATUS: Record<ImportRowStatus, { label: string; cls: string }> = {
    queued: { label: "Queued", cls: "text-slate-600 bg-slate-100" },
    running: { label: "Connecting", cls: "text-sky-700 bg-sky-50" },
    connected: { label: "Connected", cls: "text-emerald-700 bg-emerald-50" },
    updated: { label: "Updated", cls: "text-emerald-700 bg-emerald-50" },
    skipped: { label: "Skipped", cls: "text-slate-600 bg-slate-100" },
    failed: { label: "Failed", cls: "text-red-700 bg-red-50" },
    needs_signin: { label: "Needs sign-in", cls: "text-sky-700 bg-sky-50" },
    cancelled: { label: "Cancelled", cls: "text-slate-500 bg-slate-100" },
};

// A needs_signin row parked while the inbox vendor authorizes Warmbly on its domain; it resumes on its own.
export const VENDOR_AUTHORIZING = "vendor_authorizing";
export const AUTHORIZING_STATUS = { label: "Authorizing", cls: "text-sky-700 bg-sky-50" };

// Cause keys from the backend's mailcause catalogue whose fix is a new password.
const PASSWORD_CAUSE = /(_app_password_required|_bad_credentials)$|^(auth_refused|missing_password)$/;
// Cause keys whose fix is a provider sign-in rather than any password; retrying them changes nothing.
const SIGNIN_CAUSE = /^microsoft_(basic_auth_disabled|security_defaults|conditional_access|signin)$|^google_signin$/;

export function isPasswordCause(cause: string): boolean {
    return PASSWORD_CAUSE.test(cause);
}

export function isSigninCause(cause: string): boolean {
    return SIGNIN_CAUSE.test(cause);
}

/** "3, 7, 9 and 12 more" for a list of line numbers. */
export function linesText(lines: number[], total: number, shown = 6): string {
    const head = lines.slice(0, shown).join(", ");
    const more = total - Math.min(lines.length, shown);
    return more > 0 ? `${head} and ${more.toLocaleString()} more` : head;
}

export function plural(n: number, one: string, many: string): string {
    return `${n.toLocaleString()} ${n === 1 ? one : many}`;
}

/** What an import read its rows from: the file, the pasted list, the vendor account or the admin grant. */
export function importSourceName(job: { source: string; filename: string; vendor: string }): string {
    if (job.source === "paste") return "Pasted list";
    if (job.source === "vendor" || job.source === "workspace") {
        const vendor = vendorLabel(job.vendor) || (job.source === "vendor" ? "Inbox vendor" : "Admin grant");
        return job.filename && job.filename !== vendor ? `${vendor} · ${job.filename}` : vendor;
    }
    return job.filename || "File";
}

/** Where an operator turns admin connections (Google delegation, Microsoft admin consent) on. */
export const ADMIN_CONNECTIONS_DOCS = "https://docs.warmbly.com/development/configuration/#mailbox-connections";
