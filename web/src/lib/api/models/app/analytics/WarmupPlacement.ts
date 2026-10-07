// Where warmup mail landed (GET /analytics/warmup/placement, backend
// models.WarmupPlacementReport). Days are UTC; rates are percentages 0-100.

import type { DateRange } from "./CampaignAnalytics";

export type PlacementGroup = "google" | "microsoft" | "yahoo" | "other";
export type PlacementBand = "good" | "fair" | "poor" | "collecting" | "none";

export interface ObservationMetric {
    version: string;
    population: string;
    denominator_kind: string;
    unit: "percent" | "fraction";
    source: string;
    classification_policy: string;
    numerator: number;
    denominator: number;
    unresolved: number;
    window_days?: number;
    window_basis: string;
    missingness_basis: string;
    value: number | null;
    wilson_95_independence_interval: { lower: number; upper: number } | null;
}

export interface PlacementCounts {
    /** Warmup mail the mailbox sent. */
    sent: number;
    /** Classified receipts: inbox + tabs + spam, including historical rows. */
    delivered: number;
    inbox: number;
    /** Gmail category tabs (Promotions, Updates, Social, Forums). */
    tabs: number;
    spam: number;
    /** Spam placements for which a rescue action was requested, not confirmed. */
    rescued: number;
	unknown?: number;
	archived?: number;
	custom?: number;
	observed_receipts?: number;
    legacy_uninstrumented_receipts?: number;
	instrumented_receipts?: number;
    rescue_requested?: number;
    rescue_confirmed?: number | null;
    non_spam_metric?: ObservationMetric;
    /** Sent over a day ago and never seen by the recipient. */
    unconfirmed: number;
    inbox_rate: number | null;
    spam_rate: number | null;
}

/** The headline rate: trailing window, withheld below the sample floor. */
export interface PlacementRate {
    metric?: ObservationMetric;
    window_days: number;
    min_sample: number;
    // Always "major": Google, Microsoft and Yahoo recipients only.
    scope?: "major";
    delivered: number;
    inbox: number;
    tabs: number;
    spam: number;
    inbox_rate: number | null;
    band: PlacementBand;
    // Other mail hosts beside the rate: shown, never counted.
    other_delivered?: number;
    other_inbox_rate?: number | null;
}

export interface PlacementGroupCounts {
    group: PlacementGroup;
    inbox: number;
    tabs: number;
    spam: number;
    rescued: number;
	unknown?: number;
	archived?: number;
	custom?: number;
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
    date_range: DateRange;
    summary: PlacementCounts;
    rate: PlacementRate;
    daily: PlacementDay[];
    providers: PlacementProvider[];
    /** Workspace report only, worst rate first. */
    mailboxes?: PlacementMailbox[];
}
