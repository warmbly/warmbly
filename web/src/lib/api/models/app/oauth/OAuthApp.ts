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
    created_at: string;
    updated_at: string;
}

// Returned once on create / secret rotation; client_secret is shown a single time.
export interface OAuthApplicationWithSecret extends OAuthApplication {
    client_secret?: string;
}

export interface OAuthApplicationsResult {
    applications: OAuthApplication[];
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
    scopes: string[];
    state: string;
}

// An app the current user has authorized (GET /oauth/authorized-apps).
export interface OAuthAuthorizedApp {
    application_id: string;
    name: string;
    logo_url: string;
    website_url: string;
    scopes: number;
    authorized_at: string;
    last_used_at?: string;
}

export interface OAuthAuthorizedAppsResult {
    authorized_apps: OAuthAuthorizedApp[];
}
