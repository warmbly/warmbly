/**
 * What this deployment's auth can actually do, served by GET /auth/config.
 *
 * The login screen used to guess: it mounted the Turnstile widget in every
 * production build even when captcha was off server-side, rendered social
 * buttons for providers the backend had no client for, and gave a self-hoster
 * no hint that their login code had gone to a log file.
 */
export default interface AuthConfig {
    captcha: boolean;
    password_login: boolean;
    login_code: "always" | "new_device" | "off";
    registration: "true" | "false" | "invite_only";
    email_verification: boolean;
    mail_delivers: boolean;
    passkeys: boolean;
    providers: string[];
    self_hosted: boolean;
    /** False when BILLING_PROVIDER=none: the backend unlocks every feature and
     *  the org must not be presented as being on a trial or free tier. */
    billing_enabled: boolean;
    /** True while the instance has no accounts and must be claimed. */
    setup_required: boolean;
    /** registration === "invite_only", precomputed by the server so the client
     *  does not reimplement the meaning of a tri-state string. */
    invites_required: boolean;
    /** Where to send someone a deployment policy refused, not their mistake. */
    docs_url: string;
}
