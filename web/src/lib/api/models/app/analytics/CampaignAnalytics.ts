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
    // Steps whose only clicks came from automated fetchers (security
    // gateways walking the links). Not part of unique_clicks.
    machine_clicks: number
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
    // Subset of opens from automated fetchers; human opens = opens - machine_opens.
    machine_opens: number
    clicks: number
    // Contacts on this step whose only clicks were automated; not part of clicks.
    machine_clicks: number
    replies: number
    bounces: number
    // Percentages of this step's own emails_sent.
    open_rate: number
    click_rate: number
    reply_rate: number
    bounce_rate: number
}

// One slice of the engagement breakdown: distinct contacts who opened and
// clicked from that country (ISO code), client or browser, or device type.
// An empty key is "unknown".
export interface EngagementBucket {
    key: string
    opens: number
    clicks: number
}

export interface CampaignEngagementBreakdown {
    countries: EngagementBucket[]
    clients: EngagementBucket[]
    devices: EngagementBucket[]
}

export default interface CampaignAnalytics {
    campaign_id: string
    name: string
    status: string
    date_range: DateRange
    summary: CampaignSummary
    steps: SequenceStats[]
    daily_stats?: DailyStats[]
    // Where and on what people opened and clicked; human events only.
    engagement?: CampaignEngagementBreakdown | null
}
