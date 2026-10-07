export default interface Inbox {
    id: string;
    email: string;
    name: string;
    signature_plain: string;
    signature_html: string;
    signature_sync: boolean;
    signature_code: boolean;
    /** The verified provider alias this mailbox sends from. Empty, which is
     *  the default, means the mailbox's own address. Gmail only; the list of
     *  addresses it may be set to comes from /emails/:id/identity. */
    send_as_email: string;
    tags: string[];
    provider: string;
    /** Detected mailbox host (google_workspace, microsoft365, zoho, ...); "" until known. */
    mail_host?: string;
    /** How the mailbox signs in: password, app_password, oauth or delegated; "" until known. */
    auth_method?: string;
    /** The administrator's grant a delegated mailbox connects through. */
    domain_grant_id?: string | null;
    /** The inbox vendor account this mailbox was imported from. */
    vendor_connection_id?: string | null;
    /** That vendor's id (inboxkit, zapmail, ...); absent when none. */
    vendor?: string;
    /** The mailbox's own profile photo from its provider or vendor; "" when none can be read. */
    avatar_url?: string;
    status: string;
    last_synced_at: Date;
    last_id?: number | null;
    campaign_limit: number;
    test_mode?: "legacy" | "diagnostic" | "off" | null;
    test_send_enabled?: boolean;
    test_receive_enabled?: boolean;
    shared_daily_limit?: number | null;
    rolling_recipient_limit?: number | null;
    min_wait_time: number;
    reply_to: string;
    /** SMTP/IMAP only: file a copy of each sent message in the mailbox Sent folder. */
    save_to_sent: boolean;
    /** Archive, Delete and Move to inbox in the unibox move the message in the mailbox too. */
    relay_folder_moves?: boolean;
    tracking_domain: string;
    tracking_domain_verified: boolean;
    tracking_domain_verified_at?: Date | null;
    /** Opt-in: adds the open pixel and link tickets to hand-written sends. */
    track_direct_mail?: boolean;
    /**
     * Sending-domain authentication, refreshed by a background check.
     * "unknown" means not checked yet or DNS could not answer, and never gates.
     * A "failing" domain stops cold sending and warmup once it has been failing
     * since auth_failing_since for longer than the instance grace window.
     * auth_dkim is positive-only: true means a key was found at a probed
     * selector, false means none answered. Selectors are not discoverable from
     * DNS, so false is unverified, never missing.
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
    /**
     * The mailbox's own IANA zone, "" when it follows the workspace timezone.
     * Its warmup hours and sending-behaviour workday are read in this zone.
     */
    timezone?: string;
    /**
     * Where warmup mail is filed in the mail client itself: "folder" moves it
     * into warmup_folder, "inbox" leaves it where the provider put it,
     * "archive" takes it out of the inbox without a folder of its own.
     */
    warmup_placement?: "folder" | "inbox" | "archive";
    /** Folder (Gmail label) for warmup mail. Empty = the default, "Warmbly". */
    warmup_folder?: string;
    /**
     * Days warmup mail stays in this mailbox before Warmbly deletes it from
     * the warmup folder. 0 = the instance setting (30 days by default).
     */
    warmup_retention_days?: number;
    created_at: Date;
    updated_at: Date;
}
