// GET /auth/me/identities: a provider the account can sign in with.
export default interface LinkedIdentity {
    provider: "google" | "apple" | "oidc";
    label: string;
    email?: string;
    created_at: string;
    last_login_at?: string;
}
