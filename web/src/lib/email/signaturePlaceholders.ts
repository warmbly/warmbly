// Sender fields for a signature written once and saved to many mailboxes. They
// are the same {{.Sender.*}} fields a campaign email uses, but a stored
// signature is appended verbatim after templating, so they are filled in here,
// per mailbox, with the values internal/tasks/template_sender.go would render.
import { signatureToPlain } from "./signaturePlain";

export type SignatureField = "FirstName" | "LastName" | "Name" | "Email";

export const SIGNATURE_PLACEHOLDERS: readonly { field: SignatureField; token: string; label: string }[] = [
    { field: "FirstName", token: "{{.Sender.FirstName}}", label: "First name" },
    { field: "LastName", token: "{{.Sender.LastName}}", label: "Last name" },
    { field: "Name", token: "{{.Sender.Name}}", label: "Full name" },
    { field: "Email", token: "{{.Sender.Email}}", label: "Email" },
];

export interface SignatureSender {
    name: string;
    email: string;
    send_as_email?: string | null;
}

export interface SenderFields extends Record<SignatureField, string> {
    /** False when the mailbox has no sender name, so every name field is empty. */
    named: boolean;
}

const FIELD = String.raw`\{\{\s*\.Sender\.(FirstName|LastName|Name|Email)\s*\}\}`;
const TOKEN = new RegExp(FIELD, "g");
const KNOWN = new RegExp(String.raw`^${FIELD}$`);
const ANY_TOKEN = /\{\{[\s\S]*?\}\}/g;

export function senderFields(sender: SignatureSender): SenderFields {
    const words = sender.name.trim().split(/\s+/).filter(Boolean);
    return {
        FirstName: words[0] ?? "",
        LastName: words.slice(1).join(" "),
        Name: words.join(" "),
        // The From address the server renders: the send-as alias when one is set.
        Email: sender.send_as_email?.trim() || sender.email.trim(),
        named: words.length > 0,
    };
}

function escapeHtml(v: string): string {
    return v
        .replace(/&/g, "&amp;")
        .replace(/</g, "&lt;")
        .replace(/>/g, "&gt;")
        .replace(/"/g, "&quot;")
        .replace(/'/g, "&#39;");
}

/** Fills every sender field in template; html escapes the values for markup. */
export function resolveSignature(template: string, sender: SignatureSender, opts: { html: boolean }): string {
    const fields = senderFields(sender);
    return template.replace(TOKEN, (_m, field: SignatureField) => (opts.html ? escapeHtml(fields[field]) : fields[field]));
}

/** A signature template as the editor holds it: the HTML, and the plain text unless it follows the HTML. */
export interface SignatureTemplate {
    html: string;
    plain: string;
    plainSync: boolean;
}

/** One mailbox's finished signature: the HTML filled in, and the plain text rendered from it or filled on its own. */
export function fillSignature(t: SignatureTemplate, sender: SignatureSender): { html: string; plain: string } {
    const html = resolveSignature(t.html, sender, { html: true });
    return { html, plain: t.plainSync ? signatureToPlain(html) : resolveSignature(t.plain, sender, { html: false }) };
}

/** Template tokens a signature cannot fill, such as a contact field; they are sent as written. */
export function unknownPlaceholders(template: string): string[] {
    const out = new Set<string>();
    for (const m of template.matchAll(ANY_TOKEN)) {
        if (!KNOWN.test(m[0])) out.add(m[0]);
    }
    return [...out];
}
