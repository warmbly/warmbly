// GET /emails/:id/sync: where a mailbox's import stands, whether fair use is
// holding new mail, and the budget it runs under. state is null until the
// worker has reported once (a mailbox connected seconds ago).
export type SyncBackfillStatus = "pending" | "running" | "complete";

export type SyncThrottleReason =
    | "burst"
    | "hourly"
    | "daily"
    | "org_daily"
    | "priority_daily";

export interface SyncState {
    backfill_status: SyncBackfillStatus;
    backfill_cursor?: {
        page_token?: string;
        folders?: Record<string, { next?: string; uid?: number; done?: boolean }>;
        google_recovery?: {
            history_id: number;
            since: Date | string;
            page_token?: string;
            messages_done: boolean;
            stored_after?: string;
        };
    };
    backfill_synced: number;
    backfill_since?: Date;
    backfill_started_at?: Date;
    backfill_completed_at?: Date;
    throttled_until?: Date;
    throttle_reason?: SyncThrottleReason | "";
    // Live messages seen on the server but waiting on budget.
    deferred: number;
    /** Folders the last listing could not follow because the mailbox has more than the sync covers. */
    folders_skipped_cap?: number;
    /** Folders left out because the mail server listed their name more than once. */
    folders_skipped_conflict?: number;
    last_synced_at?: Date;
}

export interface SyncPolicy {
    backfill_days: number;
    backfill_messages: number;
    daily_messages: number;
    org_daily_messages: number;
    /** Folders the sync leaves alone, as the server lists them. IMAP only. */
    skip_folders?: string[];
}

/** One folder the sync has seen on the server, with the canonical folder it files under. */
export interface SyncFolder {
    name: string;
    folder: "inbox" | "sent" | "drafts" | "spam" | "trash" | "archive" | string;
}

export default interface EmailSync {
    state: SyncState | null;
    policy: SyncPolicy;
    /** The stored skip list; PUT /emails/:id/sync replaces it. */
    skip_folders: string[];
    /** What the worker last listed, INBOX first. Empty for Gmail and Outlook. */
    folders: SyncFolder[];
}
