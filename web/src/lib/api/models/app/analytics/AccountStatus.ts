// Per-mailbox status from GET /analytics/accounts/:id (backend
// models.EmailAccountStatus). Rich shape: health band, today's usage,
// warmup status, and any active errors.

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
    created_at: string;
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
    paused_at?: string | null;
    started_at: string;
    current_volume: number;
    target_volume: number;
    max_volume: number;
    reply_rate: number;
    days_active: number;
    /** Present when a recent junk placement cut the day and froze the ramp. */
    ramp_hold?: WarmupRampHold;
}

// Why today's warmup target is below the plain ramp.
export interface WarmupRampHold {
    placements: number;
    sends: number;
    last_placement_at: string;
    resumes_at: string;
}

// Warmup-pool reputation for this mailbox. Folded into health.score and also
// surfaced in detail. Present only when the mailbox is in a warmup pool.
export interface WarmupHealthInfo {
    state: "healthy" | "watch" | "throttled" | "quarantined" | "blocked";
    score: number;
    reason?: string;
    spam_score: number;
    blocked_until?: string | null;
    evaluated_at?: string | null;
}

export default interface AccountStatus {
    id: string;
    email: string;
    provider: string;
    status: string;
    last_synced_at: string | null;
    health: AccountHealth;
    errors: AccountError[];
    daily_usage: AccountDailyUsage;
    warmup_status?: WarmupStatusInfo;
    warmup_health?: WarmupHealthInfo;
    // True when the mailbox backs a live campaign — a low-volume health-check
    // warmup keeps running even if the user has warmup paused/off.
    in_campaign: boolean;
}
