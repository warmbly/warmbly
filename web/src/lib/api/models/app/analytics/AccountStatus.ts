export default interface AccountStatus {
    id: string
    email: string
    status: string
    warmup_score?: number
    daily_limit: number
    emails_sent_today: number
    health: 'good' | 'warning' | 'critical'
}
