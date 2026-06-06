import type Sequence from "./sequences/Sequence";

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

    cc: string[];
    bcc: string[];

    start_date?: Date | null;
    end_date?: Date | null;
    timezone: string;
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
    senders?: CampaignSender[];

    // Per-campaign daily ramp-up. ramp_level/ramp_level_date are server-managed.
    ramp_enabled: boolean;
    ramp_start: number;
    ramp_increment: number;
    ramp_ceiling: number;
    ramp_level: number;
    ramp_level_date?: string | null;

    // Match the sending mailbox provider to the recipient's provider.
    esp_match_mode: 'off' | 'prefer' | 'strict';

    // New-lead throttle. max_new_leads_per_day 0 = unlimited.
    max_new_leads_per_day: number;
    prioritize_new_leads: boolean;

    // Campaign-scoped tracking-domain override (honored only when verified).
    tracking_domain: string;
    tracking_domain_verified: boolean;
    tracking_domain_verified_at?: string | null;

    updated_at: Date;
    created_at: Date;

    // Extra
    analytics: null;
    sequences: Sequence[] | null;
}

// One sending window within a day, in minutes since local midnight (end > start).
export interface ScheduleInterval {
    start: number;
    end: number;
}

export interface CampaignSender {
    email_account_id: string;
    weight: number;
    last_sent_at?: string | null;
    enabled: boolean;
}

// Write shape for PUT /campaigns/:id/senders (full-replace of the pool).
export interface CampaignSenderInput {
    email_account_id: string;
    weight?: number;
    enabled?: boolean;
}

