export default interface Subscription {
    id: string
    plan_id: string
    // Nested plan summary as the API returns it (the old flat plan_name field
    // never existed on the wire).
    plan?: { name: string } | null
    status: 'active' | 'canceled' | 'past_due' | 'trialing' | 'incomplete'
    // True when an operator granted this plan rather than Stripe. Such a
    // subscription keeps whatever `status` it had, so this is the only signal
    // that the workspace is entitled. Expiry is resolved server-side.
    managed?: boolean
    current_period_start: Date
    current_period_end: Date
    cancel_at_period_end: boolean
}
