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
