// GET /subscription/trial, as trial.TrialStatus serializes it.
export default interface TrialStatus {
    is_in_trial: boolean
    trial_ends_at?: string
    days_remaining: number
    is_expired: boolean
    is_subscribed: boolean
}
