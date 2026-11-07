import type Contact from "./Contact";

export interface ContactEngagement {
    total_sent: number;
    total_opened: number;
    total_clicked: number;
    total_replied: number;
    total_bounced: number;
    total_complained: number;

    last_sent_at?: Date | null;
    last_opened_at?: Date | null;
    last_clicked_at?: Date | null;
    last_replied_at?: Date | null;
    last_bounced_at?: Date | null;

    // How the contact reads your mail: each client and device a person's
    // opens came from, most recent first.
    reads_on?: ContactReadingOrigin[];
}

export interface ContactReadingOrigin {
    client?: string;
    client_type?: "app" | "webmail";
    device_hidden?: boolean;
    device_type?: string;
    os?: string;
    browser?: string;
    opens: number;
    last_opened_at: Date;
}

export interface ContactSuppression {
    id: string;
    // "email" when the contact's own address is listed, "domain" when its
    // whole domain is; value is the list entry that matched.
    kind: "email" | "domain";
    value: string;
    reason: string;
    source: "bounce" | "complaint" | "unsubscribe" | "manual" | "import" | string;
    expires_at?: Date | null;
    created_at: Date;
}

// Where a contact first came from. Mirrors the backend CHECK; "unknown" is
// what rows created before attribution existed carry.
export type ContactSource =
    | "unknown"
    | "manual"
    | "campaign"
    | "import"
    | "sheet_sync"
    | "crm_sync"
    | "api"
    | "ai_assistant"
    | "form"
    | "automation"
    | "apollo"
    | "clay"
    | "lead_finder";

// One observed fact about the mailbox. Silence is never recorded: a contact
// who does not open or reply has said nothing about their address.
export type VerificationEvidenceKind =
    | "delivered"
    | "opened"
    | "clicked"
    | "replied"
    | "auto_replied"
    | "bounced_recipient"
    | "bounced_other";

export interface ContactVerificationEvidence {
    kind: VerificationEvidenceKind;
    detail?: string;
    observed_at: Date;
}

export interface ContactVerificationDetail {
    status: "valid" | "risky" | "invalid" | "unknown";
    confidence: number;
    reasons: string[];
    // True when real mail, not a check, decided the status.
    decisive: boolean;
    evidence: ContactVerificationEvidence[];
    // Who produced the last verdict: "probe", "provider", "imported",
    // "manual", or "" when never checked. provider_label is the verifier's
    // display name ("MillionVerifier") when there is one.
    source: "" | "probe" | "provider" | "imported" | "manual";
    provider: string;
    provider_label?: string;
    // What that check said, before real mail was weighed against it.
    check_status: "" | "valid" | "risky" | "invalid" | "unknown";
    checked_at?: Date | null;
    // Set while a re-check a member asked for waits to run.
    requested_at?: Date | null;
}

export default interface ContactDetail extends Contact {
    engagement: ContactEngagement;
    suppression?: ContactSuppression | null;
    verification?: ContactVerificationDetail | null;

    // First-touch attribution; never changes after creation.
    source: ContactSource;
    source_detail: string;
    first_seen_at: Date;
}
