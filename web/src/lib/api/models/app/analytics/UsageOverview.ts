// Mirrors the backend models.UsageOverview (GET /analytics/usage) — a nested
// snapshot of what the workspace is consuming this period.

export interface AccountsUsage {
    total: number;
    active: number;
    in_warmup: number;
    with_errors: number;
}

export interface CampaignsUsage {
    total: number;
    active: number;
    paused: number;
    draft: number;
    emails_sent: number;
}

export interface ContactsUsage {
    total: number;
    subscribed: number;
    added_today: number;
}

export interface EndpointUsage {
    endpoint: string;
    calls: number;
}

export interface APIUsage {
    total_calls: number;
    daily_limit: number;
    top_endpoints: EndpointUsage[];
}

export default interface UsageOverview {
    user_id: string;
    period: string;
    email_accounts: AccountsUsage;
    campaigns: CampaignsUsage;
    contacts: ContactsUsage;
    api: APIUsage;
}
