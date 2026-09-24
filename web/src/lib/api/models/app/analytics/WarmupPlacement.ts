// Where warmup mail landed (GET /analytics/warmup/placement, backend
// models.WarmupPlacementReport). Days are UTC; rates are percentages 0-100.

export type PlacementGroup = "google" | "microsoft" | "yahoo" | "other";
export type PlacementBand = "good" | "fair" | "poor" | "collecting" | "none";

export interface PlacementCounts {
    /** Warmup mail the mailbox sent. */
    sent: number;
    /** What recipients verifiably received: inbox + tabs + spam. */
    delivered: number;
    inbox: number;
    /** Gmail category tabs (Promotions, Updates, Social, Forums). */
    tabs: number;
    spam: number;
    /** Spam placements the recipient moved back to the inbox. */
    rescued: number;
    /** Sent over a day ago and never seen by the recipient. */
    unconfirmed: number;
    inbox_rate: number | null;
    spam_rate: number | null;
}

/** The headline rate: trailing window, withheld below the sample floor. */
export interface PlacementRate {
    window_days: number;
    min_sample: number;
    delivered: number;
    inbox: number;
    tabs: number;
    spam: number;
    inbox_rate: number | null;
    band: PlacementBand;
}

export interface PlacementGroupCounts {
    group: PlacementGroup;
    inbox: number;
    tabs: number;
    spam: number;
    rescued: number;
}

export interface PlacementDay extends PlacementCounts {
    date: string;
    rolling_inbox_rate: number | null;
    groups: PlacementGroupCounts[];
}

export interface PlacementHost extends PlacementCounts {
    host: string;
}

export interface PlacementProvider extends PlacementCounts {
    group: PlacementGroup;
    hosts: PlacementHost[];
}

export interface PlacementMailbox extends PlacementCounts {
    email_account_id: string;
    email: string;
    rate: PlacementRate;
    /** Follows the report's days; null on a day with no deliveries. */
    daily_inbox_rate: (number | null)[];
}

export default interface WarmupPlacement {
    email_account_id?: string;
    date_range: { from: string; to: string };
    summary: PlacementCounts;
    rate: PlacementRate;
    daily: PlacementDay[];
    providers: PlacementProvider[];
    /** Workspace report only, worst rate first. */
    mailboxes?: PlacementMailbox[];
}
