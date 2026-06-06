// Shared, non-component pieces of the contact-import flow. Kept in their own
// module (not exported from ImportWizard.tsx) so both the CSV ImportWizard and
// the Google-Sheet SheetSyncWizard can reuse the exact same column targets,
// dedup options, and error formatter without tripping react-refresh's
// "only export components" rule.

import toast from "react-hot-toast";
import type {
    ImportDedupStrategy,
    ImportResult,
} from "@/lib/api/client/app/contacts/importContacts";

export const STANDARD_TARGETS: { id: string; label: string }[] = [
    { id: "ignore", label: "Ignore" },
    { id: "email", label: "Email" },
    { id: "first_name", label: "First name" },
    { id: "last_name", label: "Last name" },
    { id: "company", label: "Company" },
    { id: "phone", label: "Phone" },
    { id: "subscribed", label: "Subscribed" },
    { id: "categories", label: "Categories" },
];

export const DEDUP_OPTIONS: { id: ImportDedupStrategy; label: string; hint: string }[] = [
    { id: "skip", label: "Skip existing", hint: "If a contact with this email exists, leave it alone." },
    { id: "update", label: "Update existing", hint: "Merge new values onto the existing contact." },
    {
        id: "create_duplicate",
        label: "Create duplicates",
        hint: "Force a new contact. Falls back to update if blocked by uniqueness.",
    },
];

// Extract a human-readable message from whatever the API client throws.
// Client.ts rethrows AppError (a plain object), not an Error instance —
// so `err instanceof Error` silently fails and you lose the real reason.
export function describeError(err: unknown, fallback: string): string {
    if (err && typeof err === "object") {
        const e = err as { message?: unknown; error?: unknown; status?: unknown };
        const msg = typeof e.message === "string" ? e.message.trim() : "";
        const title = typeof e.error === "string" ? e.error.trim() : "";
        const status = typeof e.status === "number" ? e.status : undefined;
        if (msg && title && msg !== title) {
            return status ? `${status} ${title}: ${msg}` : `${title}: ${msg}`;
        }
        if (msg) return status ? `${status}: ${msg}` : msg;
        if (title) return status ? `${status} ${title}` : title;
    }
    if (err instanceof Error && err.message) return err.message;
    return fallback;
}

// announceResult toasts a contact-import / sheet-sync result with the standard
// imported/updated/skipped summary (or a warning when rows failed).
export function announceResult(res: ImportResult) {
    if (res.failed === 0) {
        toast.success(
            `Imported ${res.imported} · updated ${res.updated} · skipped ${res.skipped}`,
        );
    } else {
        toast(`Synced with ${res.failed} errors`, { icon: "⚠️" });
    }
}
