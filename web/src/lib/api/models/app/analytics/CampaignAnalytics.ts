export default interface CampaignAnalytics {
    campaign_id: string
    total_sent: number
    total_opened: number
    total_clicked: number
    total_replied: number
    total_bounced: number
    open_rate: number
    click_rate: number
    reply_rate: number
    bounce_rate: number
}
