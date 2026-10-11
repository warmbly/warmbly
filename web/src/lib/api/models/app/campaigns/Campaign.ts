
export function isValidCampaignDailyLimit(value: number, savedValue?: number): boolean {
    return Number.isInteger(value) && ((value >= 3 && value <= 5000) || (value <= 0 && value === savedValue));
}

export default interface Campaign {
    id: string;

    name: string;
    description: string;
    status: string;

    stop_on_reply: boolean;
    open_tracking: boolean;
    link_tracking: boolean;
    text_only: boolean;
    daily_limit: number;
    unsubscribe_header: boolean;
    risky_emails: boolean;
    // In-body opt-out: "inherit" follows Settings > Sending.
    unsubscribe_mode: 'inherit' | 'text' | 'link' | 'off';

    cc: string[];
    bcc: string[];

    start_date?: Date | null;
    end_date?: Date | null;
    /** The schedule's own IANA zone, or "" when the campaign follows the workspace timezone. */
    timezone: string;
    /** The zone actually in use: timezone when set, else the workspace's, else UTC. Read-only. */
    effective_timezone?: string;
    days: number;
    start_time: string;
    end_time: string;

    // Per-day sending windows. 7 elements indexed by weekday (0=Sunday..6=Saturday,
    // matching the backend's time.Weekday); each day holds zero or more
    // minute-of-day intervals. When present it supersedes days/start_time/end_time.
    schedule_windows?: ScheduleInterval[][] | null;

    email_tags: string[];

    // Folder membership (folder ids). Returned by the API; set via PATCH with a
    // `folders` array (empty array clears membership, omitting leaves it as-is).
    folders?: string[];

    contact_order_by: 'created_at' | 'email' | 'name' | 'custom_field' | 'manual';
    contact_order_dir: 'asc' | 'desc';
    contact_order_field?: string;

    // ── Net-new send controls ────────────────────────────────────────────
    // Sender selection: "tags" (default) resolves mailboxes from email_tags;
    // "explicit" uses the campaign's sender pool (edited via the senders endpoint).
    sender_strategy: 'tags' | 'explicit';
    rotation_mode: 'weighted' | 'round_robin' | 'least_recently_used';
    rotate_sender_per_step: boolean;
    senders?: CampaignSender[];

    // Per-campaign daily ramp-up. ramp_level/ramp_level_date are server-managed.
    ramp_enabled: boolean;
    ramp_start: number;
    ramp_increment: number;
    ramp_ceiling: number;
    ramp_level: number;
    ramp_level_date?: Date | null;

    // Match the sending mailbox provider to the recipient's provider.
    esp_match_mode: 'off' | 'prefer' | 'strict';

    // New-lead throttle. max_new_leads_per_day 0 = unlimited.
    max_new_leads_per_day: number;
    prioritize_new_leads: boolean;

    // Delay before a contact's FIRST email, in minutes, counted from when they
    // entered the campaign. 0 sends it as soon as the schedule allows.
    entry_delay_minutes: number;

    // Keep running for new leads: out of leads, the campaign waits (idle_since
    // set) instead of finishing. Linking a segment turns it on.
    continuous: boolean;
    idle_since?: Date | null;

    // Auto-pause guardrails. Bounce and complaint rates are ceilings (pause at
    // or above); the reply rate is a floor (pause below). A rate of 0 turns its
    // rule off. guardrail_tripped_at/reason are server-owned.
    guardrail_enabled: boolean;
    guardrail_bounce_rate_max: number;
    guardrail_complaint_rate_max: number;
    guardrail_reply_rate_min: number;
    guardrail_min_sample: number;
    guardrail_window_days: number;
    guardrail_tripped_at?: Date | null;
    guardrail_reason?: string;

    // Campaign-scoped tracking-domain override (honored only when verified).
    tracking_domain: string;
    tracking_domain_verified: boolean;
    tracking_domain_verified_at?: Date | null;

    // Automatic UTM tagging of every link. Empty source/medium/campaign keep
    // the defaults (warmbly / email / the campaign name); utm_content is
    // always the link's own text.
    utm_tracking: boolean;
    utm_source: string;
    utm_medium: string;
    utm_campaign: string;

    updated_at: Date;
    created_at: Date;

    // Extra
    analytics: null;
}

// One sending window within a day, in minutes since local midnight (end > start).
export interface ScheduleInterval {
    start: number;
    end: number;
}

export interface CampaignSender {
    email_account_id: string;
    weight: number;
    last_sent_at?: Date | null;
    enabled: boolean;
}

// Write shape for PUT /campaigns/:id/senders (full-replace of the pool).
export interface CampaignSenderInput {
    email_account_id: string;
    weight?: number;
    enabled?: boolean;
}
