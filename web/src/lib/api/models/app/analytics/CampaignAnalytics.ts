import type DailyStats from "./DailyStats"

// GET /analytics/campaigns/:id — a single (un-enveloped) object that mirrors
// the backend models.CampaignAnalytics. The previous flat shape (total_sent…)
// never matched the wire body, so every field read came back undefined.

// Whole UTC days sent as RFC3339 instants, which Request revives into Dates.
export interface DateRange {
    from: Date
    to: Date
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
    // Subset of replies the reply classifier judged positive.
    positive_replies: number
    // Distinct contacts with a positive reply; a lead counts once.
    interested_leads: number
    bounces: number
    unsubscribes: number
    reply_breakdown: ReplyBreakdown
    open_rate: number
    click_rate: number
    reply_rate: number
    // Percentage of emails_sent, like reply_rate.
    positive_reply_rate: number
    bounce_rate: number
}

// Replies by classification. The human classes plus unclassified add up to
// replies; out_of_office and auto_reply are automated and never count as one.
export interface ReplyBreakdown {
    positive: number
    neutral: number
    negative: number
    unsubscribe: number
    unclassified: number
    out_of_office: number
    auto_reply: number
}

export interface SequenceStats {
    step_id: string
    name: string
    // 1-based among the campaign's email steps in canvas order, the N of "Email N".
    position: number
    emails_sent: number
    opens: number
    // Subset of opens from automated fetchers; human opens = opens - machine_opens.
    machine_opens: number
    clicks: number
    // Contacts on this step whose only clicks were automated; not part of clicks.
    machine_clicks: number
    replies: number
    positive_replies: number
    bounces: number
    // Percentages of this step's own emails_sent.
    open_rate: number
    click_rate: number
    reply_rate: number
    positive_reply_rate: number
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
    // Device and app-or-webmail together: mobile_app, desktop_app, tablet_app,
    // webmail, mobile, desktop, tablet, or hidden (a provider's image proxy).
    surfaces?: EngagementBucket[]
}

export default interface CampaignAnalytics {
    campaign_id: string
    name: string
    status: string
    // The UTC days the figures cover: the requested from/to, or for all time
    // the first send's day (creation before one) through today.
    date_range: DateRange
    summary: CampaignSummary
    steps: SequenceStats[]
    daily_stats?: DailyStats[]
    // Where and on what people opened and clicked; human events only.
    engagement?: CampaignEngagementBreakdown | null
}
