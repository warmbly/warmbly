// Per-hour campaign series (0-23). Mirrors backend models.CampaignHourlyStats.
export default interface HourlyStats {
    hour: number
    sent: number
    opens: number
    clicks: number
    replies: number
}
