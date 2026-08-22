export default interface Inbox {
    id: string;
    email: string;
    name: string;
    signature_plain: string;
    signature_html: string;
    signature_sync: boolean;
    signature_code: boolean;
    tags: string[];
    provider: string;
    status: string;
    last_synced_at: Date;
    last_id?: number | null;
    campaign_limit: number;
    min_wait_time: number;
    reply_to: string;
    /** SMTP/IMAP only: file a copy of each sent message in the mailbox Sent folder. */
    save_to_sent: boolean;
    tracking_domain: string;
    tracking_domain_verified: boolean;
    tracking_domain_verified_at?: Date | null;
    /**
     * Sending-domain authentication, refreshed by a background check.
     * "unknown" means not checked yet or DNS could not answer, and never gates.
     * A "failing" domain stops cold sending and warmup once it has been failing
     * since auth_failing_since for longer than the instance grace window.
     * auth_dkim is advisory: DKIM selectors are not discoverable from DNS.
     */
    auth_state: "unknown" | "passing" | "failing";
    auth_spf: boolean;
    auth_dkim: boolean;
    auth_dmarc: boolean;
    auth_dmarc_policy?: string;
    auth_reason?: string;
    auth_checked_at?: Date | null;
    auth_failing_since?: Date | null;
    warmup?: Date | null;
    warmup_paused_at?: Date | null;
    warmup_base: number;
    warmup_max: number;
    warmup_increase: number;
    warmup_reply_rate: number;
    warmup_pool_type?: string;
    warmup_tag?: string;
    warmup_start_time?: string;
    warmup_end_time?: string;
    warmup_days?: number;
    created_at: Date;
    updated_at: Date;
}
