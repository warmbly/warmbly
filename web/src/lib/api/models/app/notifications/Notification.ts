// In-app notification feed + per-user preferences (mirrors the Go models).

export interface ChannelPrefs {
    in_app: boolean;
    email: boolean;
    slack: boolean;
    push: boolean;
}

export interface CategoryPref {
    enabled: boolean;
    channels: ChannelPrefs;
}

// How the email channel batches alerts. Security sign-in alerts always email
// immediately regardless of cadence.
export type EmailDigestCadence = "instant" | "smart" | "hourly" | "daily";

export interface NotificationPreferences {
    inbound_reply: CategoryPref;
    inbound_out_of_office: CategoryPref;
    health_bounce: CategoryPref;
    health_complaint: CategoryPref;
    health_worker_downtime: CategoryPref;
    security_new_signin: CategoryPref;
    billing_alert: CategoryPref;
    team_activity: CategoryPref;
    email_digest: EmailDigestCadence;
}

export type NotificationCategoryKey = Exclude<keyof NotificationPreferences, "email_digest">;

export interface AppNotification {
    id: string;
    user_id: string;
    organization_id?: string | null;
    category: string;
    title: string;
    body?: string;
    link?: string;
    metadata?: Record<string, unknown>;
    read_at?: string | null;
    created_at: string;
}
