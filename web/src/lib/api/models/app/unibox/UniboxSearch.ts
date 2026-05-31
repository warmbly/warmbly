// Parameters for the inbox search endpoint (GET /unibox).
// Mirrors backend models.MailSearchParams. Empty / undefined fields are
// stripped before serialization so the URL stays clean.

export interface UniboxSearchParams {
    query?: string;        // Free text — currently matched as subject ILIKE
    from?: string;         // Sender substring
    /**
     * Selected account IDs. The server filters with email_id IN (…).
     * Use this directly when picking specific mailboxes, or set
     * `tagId` instead and let the filter UI resolve it into the set
     * of accounts that carry the tag.
     */
    accountIds?: string[];
    /**
     * UI-only convenience: when set, the filter sheet resolves the tag
     * to the matching account IDs at apply time. The server never sees
     * this field; it sees the resolved `accountIds` instead.
     */
    tagId?: string;
    unseen?: boolean;      // Only unread
    /**
     * Snoozed scope. true = only snoozed threads; undefined = exclude
     * snoozed (default inbox view); "any" = no snooze filter at all
     * (debug only).
     */
    snoozed?: true | "any";
    /** Awaiting reply: threads where the last message was from us. */
    awaitingReply?: boolean;
    since?: Date;          // From date
    until?: Date;          // To date
    sortBy?: "newest" | "oldest";
    cursor?: string;
    limit?: number;
}
