export default interface Subscription {
    id: string
    plan_id: string
    plan_name: string
    status: 'active' | 'canceled' | 'past_due' | 'trialing' | 'incomplete'
    current_period_start: Date
    current_period_end: Date
    cancel_at_period_end: boolean
}
