// One entry on the workspace suppression list, mirroring
// models.SuppressedRecipient. A "domain" entry keeps the bare host in `email`.
export type SuppressionKind = "email" | "domain";
export type SuppressionSource = "bounce" | "complaint" | "unsubscribe" | "manual" | "import";

export default interface Suppression {
    id: string;
    organization_id: string;
    email: string;
    kind: SuppressionKind;
    reason: string;
    source: SuppressionSource | string;
    campaign_id?: string | null;
    expires_at?: Date | null;
    metadata?: Record<string, unknown>;
    // What a bounce entry was about; absent on other sources and older entries.
    bounce_cause?: BounceCause | string;
    created_at: Date;
    updated_at: Date;
}

// Mirrors dsn.Cause: what a receiving server refused.
export type BounceCause = "recipient_invalid" | "mailbox_full" | "reputation_block" | "transient";

export const BOUNCE_CAUSE_LABEL: Record<string, string> = {
    recipient_invalid: "Invalid address",
    mailbox_full: "Mailbox full",
    reputation_block: "Blocked by provider",
    transient: "Temporary failure",
};

// A bounce with one of these causes was not about the address, so it was kept.
export function bounceKeptAddress(cause?: string | null): boolean {
    return cause === "mailbox_full" || cause === "reputation_block" || cause === "transient";
}

export interface SuppressionListResult {
    data: Suppression[];
    pagination: { next_cursor: string | null; has_more: boolean };
}

export interface AddSuppressionsRequest {
    entries: { value: string; reason?: string }[];
    reason?: string;
}

export interface AddSuppressionsResult {
    added: number;
    skipped: string[];
}

export const SOURCE_LABEL: Record<string, string> = {
    bounce: "Bounced",
    complaint: "Spam complaint",
    unsubscribe: "Unsubscribed",
    manual: "Added by hand",
    import: "Imported",
};

// Entries the recipient made themselves (or the mail system recorded) are
// warned about before removal; a hand-added one is the team's own call.
export function recipientTriggered(s: Suppression): boolean {
    return s.source === "unsubscribe" || s.source === "complaint" || s.source === "bounce";
}
