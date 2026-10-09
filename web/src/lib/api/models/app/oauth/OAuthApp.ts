// OAuth 2.1 authorization-server types (mirror of internal/models/oauth_app.go).

export type OAuthAppStatus = "active" | "disabled";

export interface OAuthApplication {
    id: string;
    organization_id: string;
    created_by: string;
    name: string;
    description: string;
    logo_url: string;
    website_url: string;
    client_id: string;
    redirect_uris: string[];
    // Domains this app's registered webhooks must point at. A leading dot
    // (.acme.com) matches subdomains; a bare domain is an exact match. Empty
    // forbids the app from registering webhooks.
    allowed_webhook_domains: string[];
    // The app's webhook callback URL. Must be HTTPS and its host must be inside
    // allowed_webhook_domains; empty means the app receives no events.
    webhook_url: string;
    // Event types the app subscribes to (empty = all events the granting org allows).
    webhook_events: string[];
    // Bitmask of the API permissions this app may request (same bits as API keys).
    scopes: number;
    status: OAuthAppStatus;
    /** Set when an operator suspended the app; the owner cannot lift it. */
    suspended_at?: Date;
    suspended_reason?: string;
    created_at: Date;
    updated_at: Date;
}

// Returned once on create / secret rotation; client_secret is shown a single time.
export interface OAuthApplicationWithSecret extends OAuthApplication {
    client_secret?: string;
}

export interface OAuthApplicationsResult {
    applications: OAuthApplication[];
    /** Whether an operator has blocked this workspace or person from registering and publishing apps. */
    developer_access?: { blocked: boolean; reason?: string };
}

export interface OAuthApplicationInput {
    name: string;
    description?: string;
    logo_url?: string;
    website_url?: string;
    redirect_uris: string[];
    allowed_webhook_domains?: string[];
    webhook_url?: string;
    webhook_events?: string[];
    scopes: number;
}

// The consent screen payload (GET /oauth/authorize/details).
export interface OAuthConsentInfo {
    client_id: string;
    name: string;
    description: string;
    logo_url: string;
    website_url: string;
    redirect_uri: string;
    // What approving grants: the request narrowed to the approving member's role.
    scopes: string[];
    // Requested, but outside the member's role, so not granted.
    withheld_scopes: string[];
    state: string;
    // The workspace that receives the grant.
    organization_name: string;
    // A registered app the instance features in its directory.
    verified: boolean;
    // Registered itself (RFC 7591), so nobody vouches for its name.
    self_registered: boolean;
}

// An app the current user has authorized (GET /oauth/authorized-apps).
export interface OAuthAuthorizedApp {
    application_id: string;
    name: string;
    logo_url: string;
    website_url: string;
    scopes: number;
    authorized_at: Date;
    last_used_at?: Date;
}

export interface OAuthAuthorizedAppsResult {
    authorized_apps: OAuthAuthorizedApp[];
}

// One member's authorization of an app (GET /oauth/workspace-authorizations).
export interface OAuthWorkspaceAuthorization extends OAuthAuthorizedApp {
    user_id: string;
    user_email: string;
    user_name: string;
}

export interface OAuthWorkspaceAuthorizationsResult {
    authorizations: OAuthWorkspaceAuthorization[];
}
