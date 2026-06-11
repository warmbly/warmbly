// GET /analytics/dashboard?period=7d|30d|90d — a single (un-enveloped) object
// mirroring the backend models.DashboardAnalytics. The previous flat shape
// (total_campaigns/total_contacts…) did not match the wire body.

export interface DashboardOverallStats {
    total_emails_sent: number
    total_opens: number
    // Subset of total_opens from automated fetchers (auto-opens).
    machine_opens: number
    total_clicks: number
    total_replies: number
    total_bounces: number
    open_rate: number
    click_rate: number
    reply_rate: number
    bounce_rate: number
    active_campaigns: number
    active_accounts: number
}

export interface RecentActivityItem {
    type: string // sent | opened | clicked | replied | bounced
    campaign_id: string
    campaign_name: string
    contact_email: string
    contact_id?: string
    timestamp: string
    link?: string
}

export interface TopCampaignStats {
    campaign_id: string
    name: string
    status: string
    emails_sent: number
    open_rate: number
    click_rate: number
    reply_rate: number
}

export interface AccountHealthSummary {
    total_accounts: number
    healthy_accounts: number
    warning_accounts: number
    error_accounts: number
}

export interface DashboardDailyStats {
    date: string
    sent: number
    opens: number
    clicks: number
    replies: number
}

export default interface DashboardOverview {
    period: string
    overall_stats: DashboardOverallStats
    recent_activity: RecentActivityItem[]
    top_campaigns: TopCampaignStats[]
    account_health: AccountHealthSummary
    daily_trend: DashboardDailyStats[]
}
