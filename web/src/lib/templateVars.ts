// Single source of truth for the standard contact merge fields available in
// every Go-template surface (campaign copy, templates, deal names, automation
// values). The backend renderer (internal/tasks/template.go buildTemplateData)
// exposes exactly these five standard fields plus arbitrary custom fields; keep
// this list in sync with that function. Historically this list was duplicated
// across emailPreview.ts, RichTextEditor TOKEN_META, templates/page and
// CampaignFlow — those consume this module instead.

export interface TemplateVar {
    token: string; // literal token inserted into content, e.g. "{{.Company}}"
    key: string; // bare field key, e.g. "Company"
    label: string; // friendly display name
    desc: string; // one-line description shown on hover
    sample: string; // fake value used by the client-side preview
}

export const STANDARD_VARS: TemplateVar[] = [
    { token: "{{.FirstName}}", key: "FirstName", label: "First name", desc: "The contact's first name", sample: "Alex" },
    { token: "{{.LastName}}", key: "LastName", label: "Last name", desc: "The contact's last name", sample: "Rivera" },
    { token: "{{.Email}}", key: "Email", label: "Email", desc: "The contact's email address", sample: "alex@acme.com" },
    { token: "{{.Company}}", key: "Company", label: "Company", desc: "Where the contact works", sample: "Acme" },
    { token: "{{.Phone}}", key: "Phone", label: "Phone", desc: "The contact's phone number", sample: "+1 555-0100" },
];

// The token list many surfaces already consume as `string[]`.
export const VARIABLES: string[] = STANDARD_VARS.map((v) => v.token);

// Friendly metadata keyed by token, for pickers that render label + description.
export const TOKEN_META: Record<string, { label: string; desc: string }> = Object.fromEntries(
    STANDARD_VARS.map((v) => [v.token, { label: v.label, desc: v.desc }]),
);

// Client-side preview sample context: standard fields plus a couple of common
// custom-field examples so a {{.role}} in a preview resolves to something.
export const SAMPLE: Record<string, string> = {
    ...Object.fromEntries(STANDARD_VARS.map((v) => [v.key, v.sample])),
    role: "Engineer",
    city: "Berlin",
};

const STANDARD_KEYS = new Set(STANDARD_VARS.map((v) => v.key.toLowerCase()));

// isStandardKey reports whether a (case-insensitive) key collides with a
// standard field. The backend lets a standard field win a name collision
// (template.go buildTemplateData), so the picker warns when a custom key shadows
// one.
export function isStandardKey(key: string): boolean {
    return STANDARD_KEYS.has(cleanFieldName(key).toLowerCase());
}

// cleanFieldName strips braces/leading dots so a pasted "{{.role}}" or ".role"
// still resolves to the bare key.
export function cleanFieldName(raw: string): string {
    return raw.replace(/[{}]/g, "").replace(/^\.+/, "").trim();
}

// buildToken assembles a merge token for a (possibly space-containing) key, with
// an optional `| default "…"` fallback. Keys with spaces/dashes still render as
// {{.Job Title}} — the backend rewrites those to (index . "Job Title") itself.
export function buildToken(key: string, fallback?: string | null): string {
    const k = cleanFieldName(key);
    if (!k) return "";
    if (fallback && fallback.trim()) {
        // Quotes inside the fallback would break the Go template string literal;
        // fold them to single quotes.
        const safe = fallback.replace(/"/g, "'");
        return `{{.${k} | default "${safe}"}}`;
    }
    return `{{.${k}}}`;
}

// parseToken splits a merge token back into its key and fallback for display and
// editing. Returns null when the string is not a plain field-access token (e.g.
// a conditional or a token with helpers we do not model as a chip).
export function parseToken(token: string): { key: string; fallback: string | null } | null {
    const m = token.match(/^\{\{\s*\.([A-Za-z0-9_ -]+?)\s*(?:\|\s*default\s+"([^"]*)")?\s*\}\}$/);
    if (!m) return null;
    return { key: m[1].trim(), fallback: m[2] ?? null };
}

// tokenLabel is the friendly name shown on a chip: the standard field label when
// known, otherwise the bare key.
export function tokenLabel(token: string): string {
    const meta = TOKEN_META[token];
    if (meta) return meta.label;
    const parsed = parseToken(token);
    return parsed ? parsed.key : token;
}

// FIELD_TOKEN_RE matches a bare merge-field token (optionally with a default
// fallback) but NOT control tokens like {{if .X}} / {{end}} / {{eq ...}}, so
// legacy plain content can be upgraded to chips without disturbing conditionals.
export const FIELD_TOKEN_RE = /\{\{\s*\.[A-Za-z0-9_ -]+?(?:\s*\|\s*default\s+"[^"]*")?\s*\}\}/g;

// upgradeVariableTokens wraps bare merge-field tokens in the editor HTML with the
// chip span (span[data-var]) so legacy plain content shows as chips on load. It
// is a no-op once content has been saved with chips (detected by an existing
// data-var span), which also prevents double-wrapping. The token stays as the
// span's text content, matching how VariableNode serializes back out.
export function upgradeVariableTokens(html: string): string {
    // Bail once the content already carries any chip node (variable, AI, or
    // conditional), so we never double-wrap or reach inside a chip's serialized
    // text. Legacy plain content has none of these markers.
    if (!html || html.includes("data-var") || html.includes("data-ai-var") || html.includes("data-if")) return html;
    return html.replace(FIELD_TOKEN_RE, (tok) => {
        const esc = tok.replace(/&/g, "&amp;").replace(/</g, "&lt;").replace(/>/g, "&gt;");
        return `<span data-var="">${esc}</span>`;
    });
}
