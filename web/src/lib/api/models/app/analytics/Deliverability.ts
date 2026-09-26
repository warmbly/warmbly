// Deliverability dashboard payload (mirrors models.DeliverabilityDashboard).
// Served by GET /analytics/deliverability as a bare object (no {data} envelope).

export type DeliverabilityBand = "healthy" | "warning" | "quarantine" | "blocked";

export interface DeliverabilityDailyPoint {
    date: string;
    sent: number;
    bounces: number;
    complaints: number;
    opens: number;
    clicks: number;
    replies: number;
    unsubscribes: number;
}

export interface MailboxDeliverability {
    email_account_id: string;
    email: string;
    sent: number;
    bounces: number;
    complaints: number;
    bounce_rate: number;
    complaint_rate: number;
    band: DeliverabilityBand;
}

export interface CampaignDeliverability {
    campaign_id: string;
    name: string;
    sent: number;
    bounces: number;
    complaints: number;
    bounce_rate: number;
    complaint_rate: number;
    band: DeliverabilityBand;
}

// One mail host family's seed placement: `provider` is the family id
// (gmail, microsoft365, yahoo, ...) and `label` its display name.
export interface ProviderPlacement {
    provider: string;
    label?: string;
    samples: number;
    inbox: number;
    promotions: number;
    spam: number;
    other: number;
    // Copies that never arrived within the detection window.
    missing?: number;
    inbox_rate: number;
    spam_rate: number;
}

// One recipient mail host's warmup placement: `provider` is the host id
// (google_workspace, zoho, hostinger, ...) and `label` its display name.
export interface WarmupHostPlacement {
    provider: string;
    label: string;
    delivered: number;
    spam: number;
    inbox_rate: number;
    spam_rate: number;
}

export default interface DeliverabilityDashboard {
    from: string;
    to: string;
    events_total: number;
    bounce_count: number;
    complaint_count: number;
    unsubscribe_count: number;
    reply_count: number;
    open_count: number;
    click_count: number;
    suppressed_recipients: number;
    dlq_pending: number;

    intent_positive: number;
    intent_negative: number;
    intent_out_of_office: number;
    intent_question: number;
    intent_neutral: number;
    // Machine replies that are not vacation notices: autoresponders, ticket
    // acknowledgements, bounces, delivery reports.
    intent_automated: number;

    emails_sent: number;
    bounce_rate: number;
    complaint_rate: number;
    open_rate: number;
    click_rate: number;
    reply_rate: number;

    spam_placement_rate?: number | null;
    inbox_placement_rate?: number | null;
    placement_samples: number;

    band: DeliverabilityBand;
    score: number;

    timeseries: DeliverabilityDailyPoint[];
    by_mailbox: MailboxDeliverability[];
    by_campaign: CampaignDeliverability[];
    by_provider: ProviderPlacement[];
    warmup_placement: WarmupHostPlacement[];
}
