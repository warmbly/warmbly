export default interface Register {
    email: string,
    password: string,
    turnstile: string,
    referral_code?: string,
    // Team-invitation token. It is what lets a signup through on an
    // invite_only instance, so it must survive the whole register flow.
    invite?: string
}
