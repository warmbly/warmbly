export default interface Subscription {
    id: string
    stripe_customer_id: string
    stripe_subscription_id?: string | null
    // The Stripe price being charged; equal to plan.stripe_price_id_yearly on a
    // yearly subscription.
    stripe_price_id?: string | null
    plan_id: string
    // Nested plan summary as the API returns it (the old flat plan_name field
    // never existed on the wire).
    plan?: {
        id?: string
        name: string
        price?: number
        price_yearly?: number | null
        stripe_price_id_yearly?: string | null
    } | null
    status: 'active' | 'canceled' | 'past_due' | 'trialing' | 'incomplete' | 'incomplete_expired' | 'unpaid' | 'paused'
    // True when an operator granted this plan rather than Stripe. Such a
    // subscription keeps whatever `status` it had, so this is the only signal
    // that the workspace is entitled. Expiry is resolved server-side.
    managed?: boolean
    // The workspace had the free warmup allowance before new ones stopped getting it.
    keeps_free_warmup?: boolean
    // Set on a workspace that started with the free trial; past it, the trial has ended.
    free_trial_ends_at?: string | null
    current_period_start: Date
    current_period_end: Date
    cancel_at_period_end: boolean
}
