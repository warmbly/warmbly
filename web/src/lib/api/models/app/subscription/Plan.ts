export default interface Plan {
    id: string
    name: string
    description?: string
    price_monthly: number
    price_yearly: number
    limits: {
        max_emails_per_day: number
        max_campaigns: number
        max_contacts: number
        max_team_members: number
        max_email_accounts: number
    }
    features: string[]
    is_popular?: boolean
}
