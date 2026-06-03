// Per-mailbox warmup ban status from GET /emails/:id/warmup/ban-status.
// Surfaced in the mailbox detail drawer so an owner can see why warmup was
// blocked (e.g. deleting or spam-marking warmup mail) and, when allowed,
// submit an appeal for review.
export default interface WarmupBanStatus {
    email_account_id: string;
    blocked: boolean;
    health_state: string;
    reason: string;
    blocked_at: string | null;
    blocked_until: string | null;
    can_appeal: boolean;
    pending_appeal: boolean;
}

// Result of POST /emails/:id/warmup/appeal.
export interface WarmupAppealResult {
    appeal_id: string;
}
