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

// Client-side mirror of the backend defaults merge: a response from an older
// backend (or a cached one) may miss newer categories or email_digest, and
// consumers index categories directly, so fill any gap before use.
export function normalizeNotificationPreferences(
    p: Partial<NotificationPreferences> | null | undefined,
): NotificationPreferences {
    const on: CategoryPref = { enabled: true, channels: { in_app: true, email: false, slack: false, push: true } };
    const off: CategoryPref = { enabled: false, channels: { in_app: true, email: false, slack: false, push: true } };
    const billing: CategoryPref = { enabled: true, channels: { in_app: true, email: true, slack: false, push: true } };
    const cadence: EmailDigestCadence[] = ["instant", "smart", "hourly", "daily"];
    return {
        inbound_reply: p?.inbound_reply ?? off,
        inbound_out_of_office: p?.inbound_out_of_office ?? off,
        health_bounce: p?.health_bounce ?? on,
        health_complaint: p?.health_complaint ?? on,
        health_worker_downtime: p?.health_worker_downtime ?? on,
        security_new_signin: p?.security_new_signin ?? on,
        billing_alert: p?.billing_alert ?? billing,
        team_activity: p?.team_activity ?? on,
        email_digest: p?.email_digest && cadence.includes(p.email_digest) ? p.email_digest : "smart",
    };
}

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
