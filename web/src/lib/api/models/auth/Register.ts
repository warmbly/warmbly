export default interface Register {
    email: string,
    password: string,
    turnstile: string,
    referral_code?: string
}
