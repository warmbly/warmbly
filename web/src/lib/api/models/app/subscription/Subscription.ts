export default interface Subscription {
    id: string
    plan_id: string
    // Nested plan summary as the API returns it (the old flat plan_name field
    // never existed on the wire).
    plan?: { name: string } | null
    status: 'active' | 'canceled' | 'past_due' | 'trialing' | 'incomplete'
    current_period_start: Date
    current_period_end: Date
    cancel_at_period_end: boolean
}
