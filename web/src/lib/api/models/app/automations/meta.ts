// Trigger + action vocabulary the Automations flow builder renders from. Kept in
// sync with the backend's subscribable events (catalog crmEvents/notifyEvents)
// and the per-provider action handlers.

import { EVENT_LABELS } from "@/lib/api/models/app/integrations/Integration";

// Warmbly events that can trigger an automation. Order = how they're listed.
export const TRIGGER_EVENTS: string[] = [
    "campaign.reply_received",
    "meeting.booked",
    "meeting.rescheduled",
    "meeting.canceled",
    "campaign.email_bounced",
    "campaign.unsubscribed",
    "warmup.health_changed",
    "deliverability.complaint",
];

export function triggerLabel(ev: string): string {
    return EVENT_LABELS[ev] ?? ev;
}

// Human labels for the provider action handlers (the action a step performs).
export const ACTION_LABELS: Record<string, string> = {
    "slack.notify": "Send a Slack message",
    "discord.notify": "Send a Discord message",
    "hubspot.upsert_contact": "Create / update HubSpot contact",
    "pipedrive.upsert_person": "Create / update Pipedrive person",
    "salesforce.upsert_contact": "Create / update Salesforce contact",
    "close.upsert_lead": "Create / update Close lead",
    "webhook.ping": "Send a webhook",
};

export function actionLabel(a: string): string {
    return ACTION_LABELS[a] ?? a;
}

// Per-action config field needs, so the node editor shows the right inputs.
export function actionNeedsChannel(action: string): boolean {
    return action === "slack.notify";
}
export function actionNeedsURL(action: string): boolean {
    return action === "webhook.ping";
}
// Notify-style actions accept an optional custom message template.
export function actionSupportsTemplate(action: string): boolean {
    return action === "slack.notify" || action === "discord.notify" || action === "webhook.ping";
}

// Only the reply trigger carries intent / confidence filters.
export function triggerSupportsIntentFilter(ev: string): boolean {
    return ev === "campaign.reply_received";
}
