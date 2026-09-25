// GET /campaigns/:id/send-plan. Derived through the scheduler's own gates on
// every read; nothing here is stored. The arithmetic adds up:
// configured_ceiling - sum(limits.emails) - sent_today = expected_remaining.

export type SendLimitKind =
    | "campaign_daily_limit"
    | "campaign_ramp"
    | "warmup_graduation"
    | "workspace_risk"
    | "domain_auth"
    | "resting"
    | "warmup_health_hold"
    | "other_campaigns"
    | "warmup_health_pace"
    | "mailbox_hours"
    | "sending_behavior"
    | "spacing"
    | "sending_window"
    | "not_running"
    | "org_daily_limit"
    | "new_lead_cap"
    | "leads";

export type SendBottleneck = SendLimitKind | "budget_spent" | "";

export type MailboxPlanState =
    | "sending"
    | "budget_spent"
    | "hours_closed"
    | "no_working_day"
    | "domain_auth"
    | "resting"
    | "health_hold"
    | "no_worker"
    | "window_closed";

export interface SendLimit {
    kind: SendLimitKind;
    emails: number;
    mailboxes?: number;
}

export interface SendWindow {
    sending_day: boolean;
    open_now: boolean;
    opens_at?: string;
    closes_at?: string;
    minutes_left: number;
    starts_at?: string;
    ends_at?: string;
}

export interface LeadSupply {
    due_now: number;
    due_later_today: number;
    new_leads_due_today: number;
    waiting_on_step: number;
    waiting_on_condition: number;
    held: number;
    waiting_on_sender: number;
    new_leads_started_today: number;
    max_new_leads_per_day: number;
    next_due_at?: string;
}

export interface ColdRampInfo {
    ceiling: number;
    mailbox_cap: number;
    days_to_full_cap: number;
    held: boolean;
}

export interface MailboxPlan {
    id: string;
    email: string;
    provider: string;
    configured_cap: number;
    cap_today: number;
    limited_by: "mailbox_daily_cap" | "campaign_daily_limit" | "campaign_ramp" | "warmup_graduation" | "workspace_risk";
    sent_today: number;
    sent_by_other_campaigns: number;
    expected_remaining: number;
    state: MailboxPlanState;
    reopens_at?: string;
    health?: string;
    min_gap_seconds: number;
    graduation?: ColdRampInfo;
}

export interface OrgAllowance {
    daily_limit: number;
    sent_today: number;
    remaining: number;
}

export default interface SendPlan {
    campaign_id: string;
    status: string;
    day: string;
    timezone: string;
    computed_at: string;
    configured_ceiling: number;
    projected_today: number;
    sent_today: number;
    expected_remaining: number;
    bottleneck: SendBottleneck;
    limits: SendLimit[];
    window: SendWindow;
    leads: LeadSupply;
    mailboxes: MailboxPlan[];
    organization?: OrgAllowance;
    next_wake_at?: string;
}

// What the workspace's mailboxes can send today between them, on the
// dashboard payload as capacity_today.
export interface WorkspaceSendCapacity {
    capacity: number;
    remaining_today: number;
    configured_ceiling: number;
    mailboxes: number;
    held: number;
}
