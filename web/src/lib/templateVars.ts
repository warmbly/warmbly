// Contact fields are shared across template surfaces; Sender is email-only.

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

// Keep in sync with the explicit allowlist in internal/pkg/tmplvars/sender.go.
export const SENDER_VARS: TemplateVar[] = [
    ["Name", "Sender name", "The sending mailbox's configured display name", "Jamie Morgan"],
    ["FirstName", "Sender first name", "The first word of the sending mailbox's display name", "Jamie"],
    ["LastName", "Sender last name", "The rest of the sending mailbox's display name after the first word", "Morgan"],
    ["Email", "Sender email", "The actual From address, including the chosen send-as alias", "jamie@example.com"],
    ["MailboxEmail", "Mailbox email", "The connected mailbox's own address, before any send-as alias", "jamie@example.com"],
    ["SendAsEmail", "Send-as alias", "The chosen alias, empty when none is selected"],
    ["ReplyTo", "Reply-to", "The explicit Reply-To header, empty when replies go to the sender"],
    ["SignaturePlain", "Plain signature", "The mailbox's plain-text signature"],
    ["SignatureHTML", "HTML signature", "The mailbox's HTML signature. Disable automatic signatures if placing it yourself"],
    ["SignatureSync", "Signature enabled", "Whether this mailbox automatically appends its signature (boolean)"],
    ["SignatureCode", "Signature HTML mode", "Whether the signature is edited as raw HTML (boolean)"],
    ["Provider", "Provider", "Connection provider: gmail, outlook or smtp_imap"],
    ["Status", "Mailbox status", "The mailbox's current connection status"],
    ["MailHost", "Mail host", "The detected hosting provider, such as google_workspace or microsoft365"],
    ["AuthMethod", "Authentication method", "The connection method, not credentials: password, app_password, oauth or delegated"],
    ["Vendor", "Mailbox vendor", "The inbox vendor the mailbox was imported from, when known"],
    ["AvatarURL", "Profile image URL", "The mailbox's profile image URL"],
    ["Tags", "Mailbox tags", "The mailbox's tag list. Iterate with {{range .Sender.Tags}}{{.}} {{end}}"],
    ["Timezone", "Timezone", "The mailbox timezone, falling back to the workspace timezone"],
    ["CampaignLimit", "Daily campaign cap", "The mailbox's daily cold-email cap (number)"],
    ["MinWaitTime", "Minimum send gap", "The mailbox's minimum gap between sends, in seconds (number)"],
    ["SaveToSent", "Save to Sent", "Whether SMTP/IMAP sends are saved to Sent (boolean)"],
    ["RelayFolderMoves", "Relay folder moves", "Whether inbox folder actions are relayed to the provider (boolean)"],
    ["TrackingDomain", "Tracking domain", "The mailbox's configured tracking domain"],
    ["TrackingDomainVerified", "Tracking verified", "Whether the tracking domain is verified (boolean)"],
    ["TrackingDomainVerifiedAt", "Tracking verified at", "When the tracking domain was verified, in UTC RFC 3339 format"],
    ["TrackDirectMail", "Direct-mail tracking", "Whether tracking is enabled for hand-written mail (boolean)"],
    ["AuthState", "Domain auth status", "Sending-domain authentication status: unknown, passing or failing"],
    ["AuthSPF", "SPF signal", "Whether the SPF check passed (boolean)"],
    ["AuthDKIM", "DKIM signal", "Whether a DKIM record was found. False means unverified, not missing"],
    ["AuthDMARC", "DMARC signal", "Whether a DMARC record was found (boolean)"],
    ["AuthDMARCPolicy", "DMARC policy", "The detected DMARC policy"],
    ["AuthReason", "Domain auth reason", "The sending-domain authentication diagnostic"],
    ["AuthCheckedAt", "Domain checked at", "When domain authentication was checked, in UTC RFC 3339 format"],
    ["AuthFailingSince", "Domain failing since", "When domain authentication began failing, in UTC RFC 3339 format"],
    ["Warmup", "Warmup enabled at", "The warmup start timestamp, in UTC RFC 3339 format"],
    ["WarmupPausedAt", "Warmup paused at", "The warmup pause timestamp, in UTC RFC 3339 format"],
    ["WarmupBase", "Warmup base", "The starting warmup volume (number)"],
    ["WarmupMax", "Warmup maximum", "The maximum warmup volume (number)"],
    ["WarmupIncrease", "Warmup increase", "The daily warmup volume increase (number)"],
    ["WarmupReplyRate", "Warmup reply rate", "The configured warmup reply rate (number)"],
    ["WarmupTag", "Warmup tag", "The configured warmup tag"],
    ["WarmupPoolType", "Warmup pool", "The configured warmup pool type"],
    ["WarmupStartTime", "Warmup start time", "The warmup sending window start"],
    ["WarmupEndTime", "Warmup end time", "The warmup sending window end"],
    ["WarmupDays", "Warmup days", "The warmup sending-days bitmask (number)"],
    ["WarmupPlacement", "Warmup filing", "Where warmup messages are filed"],
    ["WarmupFolder", "Warmup folder", "The configured warmup folder"],
    ["WarmupRetentionDays", "Warmup retention", "The configured retention in days; zero uses the instance default"],
    ["LastSyncedAt", "Last synced at", "When the mailbox last synced, in UTC RFC 3339 format"],
    ["CreatedAt", "Mailbox created at", "When the mailbox was added, in UTC RFC 3339 format"],
    ["UpdatedAt", "Mailbox updated at", "When mailbox settings last changed, in UTC RFC 3339 format"],
].map(([field, label, desc, sample = ""]) => ({ token: `{{.Sender.${field}}}`, key: `Sender.${field}`, label, desc, sample }));

// The recipient's opt-out link. Named because the editor treats it specially:
// applied to a text selection it becomes that text's href, so the copy can say
// what it likes and the signed URL never shows.
export const UNSUBSCRIBE_TOKEN = "{{.UnsubscribeLink}}";

// Per-send values that are not contact fields. Only campaign email bodies
// resolve these (template.go RenderTemplateWith); a deal name or automation
// value has no recipient link to offer.
export const LINK_VARS: TemplateVar[] = [
    {
        token: UNSUBSCRIBE_TOKEN,
        key: "UnsubscribeLink",
        label: "Unsubscribe link",
        desc: "This recipient's own unsubscribe link, rendered as a link labelled with your unsubscribe link text. Use it to place the opt-out in your copy instead of the footer line",
        sample: "https://example.com/unsubscribe/preview",
    },
];

// The token list many surfaces already consume as `string[]`.
export const VARIABLES: string[] = STANDARD_VARS.map((v) => v.token);
export const EMAIL_VARIABLES: string[] = [...VARIABLES, ...SENDER_VARS.map((v) => v.token)];
export const LINK_VARIABLES: string[] = LINK_VARS.map((v) => v.token);

// Friendly metadata keyed by token, for pickers that render label + description.
export const TOKEN_META: Record<string, { label: string; desc: string }> = Object.fromEntries(
    [...STANDARD_VARS, ...SENDER_VARS, ...LINK_VARS].map((v) => [v.token, { label: v.label, desc: v.desc }]),
);

// Client-side preview sample context: standard fields plus a couple of common
// custom-field examples so a {{.role}} in a preview resolves to something.
export const SAMPLE: Record<string, string> = {
    ...Object.fromEntries([...STANDARD_VARS, ...SENDER_VARS, ...LINK_VARS].map((v) => [v.key, v.sample])),
    role: "Engineer",
    city: "Berlin",
};

const STANDARD_KEYS = new Set(["sender", ...STANDARD_VARS.map((v) => v.key.toLowerCase())]);

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
    const m = token.match(/^\{\{\s*\.([A-Za-z0-9_ -]+(?:\.[A-Za-z0-9_]+)*?)\s*(?:\|\s*default\s+"([^"]*)")?\s*\}\}$/);
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

// keep in sync with FormLinkMarker in internal/pkg/tmplvars/normalize.go
export const FORM_LINK_RE = /\{\{\s*form_link:([a-z0-9]{1,64})\s*\}\}/g;

// buildFormLinkToken assembles the literal personalized-form-link marker the Go
// send pipeline resolves per recipient.
export function buildFormLinkToken(publicId: string): string {
    return `{{form_link:${publicId}}}`;
}

// parseFormLinkToken extracts the form public id from a form-link marker, or
// null when the string is not one.
export function parseFormLinkToken(token: string): string | null {
    const m = token.match(/^\{\{\s*form_link:([a-z0-9]{1,64})\s*\}\}$/);
    return m ? m[1] : null;
}

// FIELD_TOKEN_RE matches a bare merge-field token (optionally with a default
// fallback) but NOT control tokens like {{if .X}} / {{end}} / {{eq ...}}, so
// legacy plain content can be upgraded to chips without disturbing conditionals.
export const FIELD_TOKEN_RE = /\{\{\s*\.[A-Za-z0-9_ -]+(?:\.[A-Za-z0-9_]+)*?(?:\s*\|\s*default\s+"[^"]*")?\s*\}\}/g;

// upgradeVariableTokens wraps bare merge-field and form-link tokens in the
// editor HTML with their chip spans (span[data-var] / span[data-form-link]) so
// legacy plain content shows as chips on load. It is a no-op once content has
// been saved with chips (detected by an existing chip span), which also
// prevents double-wrapping. The token stays as the span's text content,
// matching how VariableNode/FormLinkNode serialize back out.
export function upgradeVariableTokens(html: string): string {
    // Bail once the content already carries any chip node (variable, AI,
    // conditional, or form link), so we never double-wrap or reach inside a
    // chip's serialized text. Legacy plain content has none of these markers.
    if (
        !html ||
        html.includes("data-var") ||
        html.includes("data-ai-var") ||
        html.includes("data-if") ||
        html.includes("data-form-link")
    )
        return html;
    // Text nodes only. A token can legitimately live in an attribute (an
    // <a href="{{.UnsubscribeLink}}"> the author wrote), and wrapping that one
    // in a span would break the tag.
    return html.replace(HTML_CHUNK_RE, (chunk) =>
        chunk.startsWith("<")
            ? chunk
            : chunk
                  .replace(FIELD_TOKEN_RE, (tok) => {
                      const esc = tok.replace(/&/g, "&amp;").replace(/</g, "&lt;").replace(/>/g, "&gt;");
                      return `<span data-var="">${esc}</span>`;
                  })
                  .replace(FORM_LINK_RE, (tok, publicId: string) => `<span data-form-link="${publicId}">${tok}</span>`),
    );
}

// HTML_CHUNK_RE splits a body into alternating tags and text runs. A tag ends
// at the first ">" OUTSIDE a quoted attribute value, so `<a title="x > y"
// href="…">` stays one tag: reading the quoted ">" as the end split the tag and
// let the href be treated as text. A stray "<" matches on its own and is left
// alone rather than swallowing the rest of the body.
export const HTML_CHUNK_RE = /<(?:"[^"]*"|'[^']*'|[^>"'])*>|[^<]+|</g;
