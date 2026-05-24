export default interface WarmupAnalytics {
    total_accounts: number
    warming_accounts: number
    warmed_accounts: number
    daily_stats: { date: string; sent: number; received: number }[]
}
