// Per-day campaign series. Mirrors the backend models.CampaignDailyStats
// (and DashboardDailyStats) JSON tags exactly — note the backend uses
// `opens`/`clicks`/`replies`, not `opened`/`clicked`/`replied`.
export default interface DailyStats {
    date: string
    sent: number
    opens: number
    clicks: number
    replies: number
}
