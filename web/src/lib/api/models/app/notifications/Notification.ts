// In-app notification feed + per-user preferences (mirrors the Go models).

export interface ChannelPrefs {
    in_app: boolean;
    email: boolean;
    slack: boolean;
}

export interface CategoryPref {
    enabled: boolean;
    channels: ChannelPrefs;
}

export interface NotificationPreferences {
    inbound_reply: CategoryPref;
    inbound_out_of_office: CategoryPref;
    health_bounce: CategoryPref;
    health_complaint: CategoryPref;
    health_worker_downtime: CategoryPref;
    security_new_signin: CategoryPref;
}

export type NotificationCategoryKey = keyof NotificationPreferences;

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
