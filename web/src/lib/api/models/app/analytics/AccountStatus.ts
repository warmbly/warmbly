// Per-mailbox status from GET /analytics/accounts/:id (backend
// models.EmailAccountStatus). Rich shape: health band, today's usage,
// warmup status, and any active errors.

import type { PlacementRate } from "./WarmupPlacement";

export interface AccountHealth {
    status: "healthy" | "warning" | "error";
    score: number; // 0-100
    issues?: string[];
}

export interface AccountError {
    id: string;
    error_code: string;
    severity: string;
    title: string;
    message: string;
    action_required?: string;
    created_at: Date;
    operation?: "send" | "sync" | "unknown";
}

export interface AccountDailyUsage {
    date: string;
    campaign_sent: number;
    campaign_limit: number;
    warmup_sent?: number;
    warmup_limit?: number;
}

export interface WarmupStatusInfo {
    enabled: boolean;
    paused: boolean;
    paused_at?: Date | null;
    started_at: Date;
    current_volume: number;
    target_volume: number;
    max_volume: number;
    /** Configured percent of warmup sends that should receive a synthetic reply. */
    reply_rate: number;
    days_active: number;
    /** Present while a recent junk placement is holding the ramp. */
    ramp_hold?: WarmupRampHold;
    /** Present while today's target is capped by the partners the mailbox can still reach. */
    partner_limit?: WarmupPartnerLimit;
    /** Present while the newest warmup send was refused and none has gone out since. */
    send_failure?: WarmupSendFailure;
    /** When the next warmup send is queued for; absent while nothing is queued. */
    next_send_at?: Date;
}

// Why a warmup send failed: the mail server's answer when it gave one.
export interface WarmupSendFailure {
    message: string;
    at: string;
    kind?: "mailbox_loading";
    first_failure_at?: string;
}

// A target held below the ramp because a mailbox never writes to the same
// partner twice in a day.
export interface WarmupPartnerLimit {
    reachable: number;
    ramp_target: number;
}

// Why the warmup ramp is not climbing. Present for the whole freeze;
// volume_cut says whether today's volume is also reduced, which ends sooner.
export interface WarmupRampHold {
    placements: number;
    sends: number;
    volume_cut: boolean;
    resumes_at: Date;
}

// Warmup-pool reputation for this mailbox. Folded into health.score and also
// surfaced in detail. Present only when the mailbox is in a warmup pool.
export interface WarmupHealthInfo {
    /** The pool the mailbox warms in. */
    pool_type?: "premium" | "free";
    /** "cloud" when Warmbly Cloud warms the mailbox and reported this standing. */
    source?: "cloud";
    state: "healthy" | "watch" | "throttled" | "quarantined" | "blocked";
    score: number;
    /** @deprecated Always 0 since the warmup spam score was retired; read score and reason. */
    spam_score: number;
    reason?: string;
    blocked_until?: Date | null;
    evaluated_at?: Date | null;
    /** Distinct warmup partners over the last 7 days: mailboxes, their domains, and the workspaces behind them. */
    partner_mailboxes_7d: number;
    partner_domains_7d: number;
    partner_organizations_7d: number;
    /** The receiving side over the same window: verified warmup arrivals and the distinct partners they came from. */
    received_7d: number;
    senders_7d: number;
}

export type SendPauseReason =
    | "rate_limited"
    | "daily_limit"
    | "sender_blocked"
    | "retry"
    | "domain_auth"
    | "permanent"
    | "authentication"
    | "";

// Why a provider stopped this mailbox sending, and when it starts again.
export interface SendPause {
    reason: SendPauseReason;
    /** True for a hold that waits for a fix or a re-probe; false for a timed pause. */
    held: boolean;
    /** When a timed pause ends. */
    until?: string;
    /** When Warmbly lets a held mailbox try one more send on its own. */
    retry_at?: string;
    provider_message?: string;
    /** POST /emails/:id/resume-sending can lift it; an authentication hold needs a reconnect. */
    resumable: boolean;
}

export default interface AccountStatus {
    id: string;
    email: string;
    provider: string;
    status: string;
    last_synced_at: Date | null;
    health: AccountHealth;
    errors: AccountError[];
    daily_usage: AccountDailyUsage;
    warmup_status?: WarmupStatusInfo;
    /** Present only while graduation holds the cold cap below the configured one. */
    cold_ramp?: ColdRampInfo;
    /** Names a send safety hold; absent while sending is not held. "authentication" and "permanent" clear on a successful reconnect; a Gmail or Outlook "permanent" also clears once the mailbox syncs again; "domain_auth" clears once the sending domain's check passes. */
    send_recovery_reason?: "authentication" | "permanent" | "domain_auth" | "conflict" | "unknown";
    /** A provider's pause on this mailbox's sending; absent while it sends normally. */
    send_pause?: SendPause;
    /** Present only when the mailbox is NOT in cold rotation. */
    send_lifecycle?: SendLifecycleState;
    warmup_health?: WarmupHealthInfo;
    /** Where warmup mail landed over the trailing week; also caps health.score. Absent with no deliveries. */
    warmup_placement?: PlacementRate;
    // True when the mailbox backs a live campaign — a low-volume health-check
    // warmup keeps running even if the user has warmup paused/off.
    in_campaign: boolean;
}

// Why the cold cap is below the number configured on the mailbox.
export interface ColdRampInfo {
    ceiling: number;
    mailbox_cap: number;
    days_to_full_cap: number;
    held: boolean;
}

export type SendLifecycle = "active" | "resting" | "reserve";

// Why a mailbox is not being offered cold sends.
export interface SendLifecycleState {
    state: SendLifecycle;
    since?: Date;
    reason?: string;
}
