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

// The email-channel bundling window bounds (also returned by the API so the
// control never hardcodes them): pending notification emails hold for the
// user's window, then flush as one bundled email. The 30 minute floor is
// deliberate — there is no per-event email mode. Security sign-in alerts
// always email immediately.
export const EMAIL_WINDOW_MIN_MINUTES = 30;
export const EMAIL_WINDOW_MAX_MINUTES = 1440;

export interface NotificationPreferences {
    inbound_reply: CategoryPref;
    inbound_positive_reply: CategoryPref;
    inbound_out_of_office: CategoryPref;
    health_bounce: CategoryPref;
    health_complaint: CategoryPref;
    health_worker_downtime: CategoryPref;
    security_new_signin: CategoryPref;
    billing_alert: CategoryPref;
    team_activity: CategoryPref;
    campaign_paused: CategoryPref;
    health_domain_auth: CategoryPref;
    placement_finished: CategoryPref;
    placement_alert: CategoryPref;
    inbox_action_required: CategoryPref;
    email_digest_minutes: number;
}

export type NotificationCategoryKey = Exclude<keyof NotificationPreferences, "email_digest_minutes">;

export interface NotificationCategoryDef {
    key: NotificationCategoryKey;
    label: string;
    hint: string;
}

// Every category, grouped as the settings page shows them. The Slack routing
// panel reads the same list, so a new category shows up in both.
export const NOTIFICATION_CATEGORY_GROUPS: { id: string; label: string; categories: NotificationCategoryDef[] }[] = [
    {
        id: "inbound",
        label: "Inbound activity",
        categories: [
            { key: "inbound_positive_reply", label: "Positive reply", hint: "A recipient answered a cold email with interest. On by default." },
            { key: "inbound_reply", label: "Reply received", hint: "A person replied to a cold email, whatever they said. When on, it covers positive replies too. Automatic replies have their own setting below." },
            { key: "inbound_out_of_office", label: "Out-of-office detected", hint: "An auto-responder hit one of your sends." },
        ],
    },
    {
        id: "health",
        label: "Health",
        categories: [
            { key: "health_bounce", label: "Bounce detected", hint: "A campaign starts bouncing. Notifies the campaign owner." },
            { key: "health_complaint", label: "Spam complaint", hint: "Any complaint event on one of your campaigns." },
            { key: "health_worker_downtime", label: "Worker downtime", hint: "A sender worker stops responding." },
            { key: "inbox_action_required", label: "Mail that needs action", hint: "A mailbox received automated mail that needs someone to act, like a failed payment, a suspended account or a suspicious sign-in. Goes to members who manage mailboxes and use the inbox." },
            { key: "health_domain_auth", label: "Domain authentication failing", hint: "A sending domain lost its SPF or DMARC record. Cold sending and warmup stop from it if it is not fixed." },
            { key: "campaign_paused", label: "Campaign auto-paused", hint: "A guardrail stopped a campaign because its bounce, complaint, or reply rate left the band." },
            { key: "placement_alert", label: "Placement monitor alert", hint: "A campaign's scheduled placement test found less of its mail in the inbox than its alert threshold." },
            { key: "placement_finished", label: "Placement test finished", hint: "A placement test you started has a verdict for every copy." },
        ],
    },
    {
        id: "security",
        label: "Security",
        categories: [
            { key: "security_new_signin", label: "New sign-in", hint: "Your account was accessed from a device you haven't used before." },
        ],
    },
    {
        id: "billing",
        label: "Billing",
        categories: [
            { key: "billing_alert", label: "Trial and billing alerts", hint: "Your trial is about to expire or your workspace was paused. Goes to members who manage billing." },
        ],
    },
    {
        id: "team",
        label: "Team",
        categories: [
            { key: "team_activity", label: "Teammate joined your workspace", hint: "A new member accepted an invite. Goes to members who manage the team." },
        ],
    },
];

export const NOTIFICATION_CATEGORY_KEYS: NotificationCategoryKey[] = NOTIFICATION_CATEGORY_GROUPS.flatMap((g) =>
    g.categories.map((c) => c.key),
);

// Email-channel bounds from the deployment: the window range clients should
// offer, and the rolling 24h per-user email budget (0 = unlimited).
export interface EmailDeliveryInfo {
    min_minutes: number;
    max_minutes: number;
    daily_cap: number;
}

export interface NotificationPreferencesEnvelope {
    preferences: NotificationPreferences;
    email_delivery?: EmailDeliveryInfo;
}

// Client-side mirror of the backend defaults merge: a response from an older
// backend (or a cached one) may miss newer categories or email_digest, and
// consumers index categories directly, so fill any gap before use.
export function normalizeNotificationPreferences(
    p: Partial<NotificationPreferences> | null | undefined,
): NotificationPreferences {
    const on: CategoryPref = { enabled: true, channels: { in_app: true, email: false, slack: false, push: true } };
    const off: CategoryPref = { enabled: false, channels: { in_app: true, email: false, slack: false, push: true } };
    const billing: CategoryPref = { enabled: true, channels: { in_app: true, email: true, slack: false, push: true } };
    const minutes = p?.email_digest_minutes ?? EMAIL_WINDOW_MIN_MINUTES;
    return {
        inbound_reply: p?.inbound_reply ?? off,
        inbound_positive_reply: p?.inbound_positive_reply ?? on,
        inbound_out_of_office: p?.inbound_out_of_office ?? off,
        health_bounce: p?.health_bounce ?? on,
        health_complaint: p?.health_complaint ?? on,
        health_worker_downtime: p?.health_worker_downtime ?? on,
        security_new_signin: p?.security_new_signin ?? on,
        billing_alert: p?.billing_alert ?? billing,
        team_activity: p?.team_activity ?? on,
        // Emails by default, like billing: a campaign the platform stopped by
        // itself has to reach whoever can restart it.
        campaign_paused: p?.campaign_paused ?? billing,
        // Emails by default too: a sending domain the platform will stop
        // sending from has to reach whoever can edit the DNS.
        health_domain_auth: p?.health_domain_auth ?? billing,
        placement_finished: p?.placement_finished ?? on,
        // Emails by default: a campaign landing in spam has to reach whoever
        // can fix it even when nobody has the dashboard open.
        placement_alert: p?.placement_alert ?? billing,
        // Emails by default: a mailbox about to lose its subscription has to
        // reach whoever can fix it even when nobody reads that inbox.
        inbox_action_required: p?.inbox_action_required ?? billing,
        email_digest_minutes: Math.min(Math.max(minutes, EMAIL_WINDOW_MIN_MINUTES), EMAIL_WINDOW_MAX_MINUTES),
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
    read_at?: Date | null;
    created_at: Date;
}
