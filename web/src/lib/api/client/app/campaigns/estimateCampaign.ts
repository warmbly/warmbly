import Request from "../../Request";

// Wire shape for POST /campaigns-estimate: an audience (segments) against a
// sender pool (tags, or every active mailbox when empty) under a per-mailbox
// daily limit, a sending window and the follow-up waits. Read-only; nothing
// is created.
export interface CampaignEstimateInput {
    segment_ids: string[];
    email_tag_ids?: string[];
    daily_limit?: number;
    days?: number;
    timezone?: string;
    // RFC 3339. Omit to start now.
    start_date?: string;
    // "HH:MM" daily window.
    start_time?: string;
    end_time?: string;
    // Each follow-up's wait in days, in order. Empty is a single email.
    step_waits?: number[];
    // A saved draft: its leads count too, and its own senders and windows apply.
    campaign_id?: string;
}

export type EstimateBottleneck =
    | ""
    | "campaign_limit"
    | "warmup_graduation"
    | "spacing"
    | "other_campaigns"
    | "health"
    | "held"
    | "workspace_risk"
    | "org_daily_limit"
    | "sending_behavior";

export type EstimateSenderState =
    | "ready"
    | "ramping"
    | "throttled"
    | "health_hold"
    | "send_recovery"
    | "send_cooldown"
    | "send_authority"
    | "domain_auth"
    | "resting"
    | "no_worker";

export interface CampaignEstimateDay {
    date: string;
    sending_day: boolean;
    capacity: number;
    sends: number;
    first_emails: number;
    follow_ups: number;
    warmup: number;
}

export interface CampaignEstimateSender {
    id: string;
    email: string;
    provider: string;
    state: EstimateSenderState;
    first_day_cap: number;
    steady_cap: number;
    warmup_per_day: number;
    full_cap_at: Date | null;
}

export interface CampaignEstimateResult {
    recipients: number;
    mailboxes: number;
    // Pool ceiling per sending day under the campaign limit, today.
    daily_capacity: number;
    // What the pool can still send today.
    remaining_today: number;
    // Null when the pool has no capacity, the audience is empty, or the send
    // outruns the two-year horizon.
    sending_days: number | null;
    estimated_finish_at: Date | null;
    steps: number;
    total_sends: number;
    // The day the last contact gets their first email.
    first_touch_finish_at: Date | null;
    // Sending-day capacity once every mailbox has graduated from warmup.
    steady_capacity: number;
    full_capacity_at: Date | null;
    ramping: number;
    held: number;
    // Warmup mail the pool keeps sending alongside the campaign.
    warmup: { mailboxes: number; per_day: number };
    other_campaigns_per_day: number;
    bottleneck: EstimateBottleneck;
    timeline: CampaignEstimateDay[];
    senders: CampaignEstimateSender[];
}

export default async function estimateCampaign(input: CampaignEstimateInput): Promise<CampaignEstimateResult> {
    return await Request<CampaignEstimateResult>({
        method: "POST",
        url: `/campaigns-estimate`,
        data: input,
        authorization: true,
    });
}
