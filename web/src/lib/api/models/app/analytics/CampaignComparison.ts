import type { DateRange } from "./CampaignAnalytics"

// GET /analytics/campaigns/compare?ids=a,b,c — a single (un-enveloped) object
// mirroring backend models.CampaignComparison. Compare returns an object with
// a `campaigns` array, NOT a bare array.

export interface CampaignComparisonItem {
    campaign_id: string
    name: string
    status: string
    emails_sent: number
    // Sends that carried an open pixel and tracked links; open_rate and
    // click_rate are of these. Absent from a server older than the field.
    open_tracked_sent?: number
    click_tracked_sent?: number
    open_rate: number
    click_rate: number
    reply_rate: number
    bounce_rate: number
}

export default interface CampaignComparison {
    campaigns: CampaignComparisonItem[]
    period: DateRange
}
