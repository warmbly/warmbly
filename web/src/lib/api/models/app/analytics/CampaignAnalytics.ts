import type DailyStats from "./DailyStats"

// GET /analytics/campaigns/:id — a single (un-enveloped) object that mirrors
// the backend models.CampaignAnalytics. The previous flat shape (total_sent…)
// never matched the wire body, so every field read came back undefined.

export interface DateRange {
    from: string
    to: string
}

export interface CampaignSummary {
    total_contacts: number
    emails_sent: number
    emails_pending: number
    unique_opens: number
    // Subset of unique_opens from automated fetchers (Apple MPP prefetch
    // and UA-less clients). Human opens = unique_opens - machine_opens.
    machine_opens: number
    unique_clicks: number
    replies: number
    bounces: number
    unsubscribes: number
    open_rate: number
    click_rate: number
    reply_rate: number
    bounce_rate: number
}

export interface SequenceStats {
    step_id: string
    name: string
    position: number
    emails_sent: number
    opens: number
    clicks: number
    replies: number
    bounces: number
}

export default interface CampaignAnalytics {
    campaign_id: string
    name: string
    status: string
    date_range: DateRange
    summary: CampaignSummary
    steps: SequenceStats[]
    daily_stats?: DailyStats[]
}
