import type { WorkspaceSendCapacity } from "@/lib/api/models/app/campaigns/SendPlan"
import type { EngagementOrigin } from "@/lib/api/models/app/contacts/ContactTimelineEvent"

// GET /analytics/dashboard?period=7d|30d|90d[&campaign_ids][&folder_ids] — a single (un-enveloped) object
// mirroring the backend models.DashboardAnalytics. The previous flat shape
// (total_campaigns/total_contacts…) did not match the wire body.

export interface DashboardOverallStats {
    total_emails_sent: number
    total_opens: number
    // Subset of total_opens from automated fetchers (auto-opens).
    machine_opens: number
    total_clicks: number
    // Steps clicked only by automated fetchers; not part of total_clicks.
    machine_clicks: number
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
    timestamp: Date
    link?: string
    // Client, device and location of a person's open or click, when logged.
    origin?: EngagementOrigin
    // The mailbox the step went out from, which a reply credits even when it
    // landed in a shared reply inbox.
    sender_id?: string
    sender_email?: string
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
    // What the workspace's mailboxes can send today under the scheduler's
    // clamps; the sidebar meter's denominator. Absent when not computed.
    capacity_today?: WorkspaceSendCapacity
    // The campaign filter the campaign sections were computed for; absent
    // when they cover the whole workspace.
    scope?: DashboardScope
}

// What the page asks for: campaign and folder ids, unioned by the server.
export interface DashboardCampaignFilter {
    campaigns: string[]
    folders: string[]
}

export interface DashboardScopeItem {
    id: string
    name: string
}

// The requested campaigns and folders that belong to the workspace, and how
// many distinct campaigns they cover together.
export interface DashboardScope {
    campaigns: DashboardScopeItem[]
    folders: DashboardScopeItem[]
    campaign_count: number
}
