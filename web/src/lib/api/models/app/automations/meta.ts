// Trigger + action vocabulary the Automations flow builder renders from. Kept in
// sync with the backend's subscribable events (catalog crmEvents/notifyEvents)
// and the per-provider action handlers.

import { EVENT_LABELS, REPLY_INTENT_OPTIONS } from "@/lib/api/models/app/integrations/Integration";
import type { AutomationCondition } from "@/lib/api/models/app/automations/Automation";

// Warmbly events that can trigger an automation. Order = how they're listed.
// "campaign.action" is the manual / campaign-launched trigger: it never fires on
// a real event, only via a campaign "Run automation" step (RunAutomationByID).
export const TRIGGER_EVENTS: string[] = [
    "campaign.reply_received",
    "meeting.booked",
    "meeting.rescheduled",
    "meeting.canceled",
    "campaign.email_bounced",
    "campaign.unsubscribed",
    "warmup.health_changed",
    "deliverability.complaint",
    "inbound.webhook",
    "campaign.action",
];

export function triggerLabel(ev: string): string {
    return EVENT_LABELS[ev] ?? ev;
}

// The inbound-webhook trigger fires when an external system POSTs to this
// automation's unique URL; the JSON body becomes the event payload, so its
// fields are caller-defined rather than fixed.
export function triggerIsInboundWebhook(ev: string): boolean {
    return ev === "inbound.webhook";
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
    // Native (Warmbly built-in) actions — no external connection needed.
    "warmbly.add_tag": "Add a tag",
    "warmbly.remove_tag": "Remove a tag",
    "warmbly.create_task": "Create a task",
    "warmbly.create_deal": "Create a deal",
    "warmbly.move_deal_stage": "Move the deal stage",
    "warmbly.unsubscribe": "Unsubscribe the contact",
    "warmbly.run_automation": "Run another automation",
    "warmbly.label_email": "Label the email",
    "warmbly.set_variables": "Set variables",
    "warmbly.fire_event": "Fire event",
};

export function actionLabel(a: string): string {
    return ACTION_LABELS[a] ?? a;
}

// Native (Warmbly-internal) actions operate on the event's contact directly,
// with no external connection. The "__native__" sentinel is the connection-select
// value that switches the action editor into native mode.
export const NATIVE_CONNECTION = "__native__";

export const NATIVE_ACTIONS: string[] = [
    "warmbly.add_tag",
    "warmbly.remove_tag",
    "warmbly.create_task",
    "warmbly.create_deal",
    "warmbly.move_deal_stage",
    "warmbly.unsubscribe",
    "warmbly.run_automation",
    "warmbly.label_email",
    "warmbly.set_variables",
    "warmbly.fire_event",
];

export function isNativeAction(a: string): boolean {
    return a.startsWith("warmbly.");
}

// What config a native action needs, so the editor shows the right picker.
export function nativeActionNeeds(
    action: string,
): "tag" | "label" | "deal" | "task" | "automation" | "vars" | "event" | "none" {
    switch (action) {
        case "warmbly.add_tag":
        case "warmbly.remove_tag":
            return "tag";
        case "warmbly.label_email":
            return "label";
        case "warmbly.create_deal":
        case "warmbly.move_deal_stage":
            return "deal";
        case "warmbly.create_task":
            return "task";
        case "warmbly.run_automation":
            return "automation";
        case "warmbly.set_variables":
            return "vars";
        case "warmbly.fire_event":
            return "event";
        default:
            return "none";
    }
}

// Which triggers carry an inbox thread, so a "label email" action has something
// to label (the reply event payload includes thread_id + the mailbox owner).
export function triggerCarriesThread(ev: string): boolean {
    return ev === "campaign.reply_received";
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

// --- Condition (IF) vocabulary — data-driven, per trigger -------------------
// An IF tests a key from the trigger's event payload with an operator. Each
// trigger exposes the fields its payload actually carries (so conditions are
// always meaningful), plus a universal "Random split". Adding an event here +
// to TRIGGER_VARIABLES is all it takes for new triggers to get full conditions.

export type ConditionValueType = "string" | "number" | "bool" | "enum";

export interface TriggerFieldDef {
    key: string;
    label: string;
    type: ConditionValueType;
    options?: { value: string; label: string }[];
    defaultOperator: string;
}

// Sentinel field key for the random-split pseudo-condition.
export const RANDOM_FIELD_KEY = "__random__";

// Sentinel field key for the advanced free-form expression condition.
export const EXPRESSION_FIELD_KEY = "__expression__";

export const WARMUP_STATES = [
    { value: "healthy", label: "Healthy" },
    { value: "watch", label: "Watch" },
    { value: "throttled", label: "Throttled" },
    { value: "quarantined", label: "Quarantined" },
    { value: "blocked", label: "Blocked" },
];

const MEETING_FIELDS: TriggerFieldDef[] = [
    { key: "source", label: "Source", type: "string", defaultOperator: "equals" },
    { key: "invitee_email", label: "Invitee email", type: "string", defaultOperator: "contains" },
    { key: "event_name", label: "Meeting name", type: "string", defaultOperator: "contains" },
    { key: "contact_id", label: "Matched contact", type: "string", defaultOperator: "exists" },
];

const DELIVERABILITY_FIELDS: TriggerFieldDef[] = [
    { key: "event_type", label: "Event type", type: "string", defaultOperator: "equals" },
    { key: "provider", label: "Provider", type: "string", defaultOperator: "equals" },
    { key: "reason", label: "Reason", type: "string", defaultOperator: "contains" },
    { key: "contact_email", label: "Contact email", type: "string", defaultOperator: "contains" },
    { key: "campaign_id", label: "From a campaign", type: "string", defaultOperator: "exists" },
];

export const TRIGGER_FIELDS: Record<string, TriggerFieldDef[]> = {
    "campaign.reply_received": [
        { key: "intent", label: "Reply intent", type: "enum", options: REPLY_INTENT_OPTIONS, defaultOperator: "equals" },
        { key: "confidence", label: "Classifier confidence", type: "number", defaultOperator: "gte" },
        { key: "contact_email", label: "Contact email", type: "string", defaultOperator: "contains" },
        { key: "subject", label: "Subject", type: "string", defaultOperator: "contains" },
        { key: "contact_id", label: "Matched contact", type: "string", defaultOperator: "exists" },
    ],
    "meeting.booked": MEETING_FIELDS,
    "meeting.rescheduled": MEETING_FIELDS,
    "meeting.canceled": MEETING_FIELDS,
    "campaign.email_bounced": DELIVERABILITY_FIELDS,
    "deliverability.bounce": DELIVERABILITY_FIELDS,
    "deliverability.complaint": DELIVERABILITY_FIELDS,
    "campaign.unsubscribed": [
        { key: "source", label: "Unsubscribe source", type: "string", defaultOperator: "equals" },
        { key: "contact_email", label: "Contact email", type: "string", defaultOperator: "contains" },
        { key: "campaign_id", label: "From a campaign", type: "string", defaultOperator: "exists" },
    ],
    "warmup.health_changed": [
        { key: "new_state", label: "New state", type: "enum", options: WARMUP_STATES, defaultOperator: "equals" },
        { key: "previous_state", label: "Previous state", type: "enum", options: WARMUP_STATES, defaultOperator: "equals" },
        { key: "email", label: "Mailbox", type: "string", defaultOperator: "contains" },
    ],
    "campaign.action": [
        { key: "contact_email", label: "Contact email", type: "string", defaultOperator: "contains" },
        { key: "company", label: "Company", type: "string", defaultOperator: "contains" },
        { key: "campaign_id", label: "From a campaign", type: "string", defaultOperator: "exists" },
        { key: "contact_id", label: "Matched contact", type: "string", defaultOperator: "exists" },
    ],
};

const GENERIC_FIELDS: TriggerFieldDef[] = [
    { key: "contact_email", label: "Contact email", type: "string", defaultOperator: "contains" },
    { key: "contact_id", label: "Matched contact", type: "string", defaultOperator: "exists" },
];

const RANDOM_FIELD: TriggerFieldDef = {
    key: RANDOM_FIELD_KEY,
    label: "Random split",
    type: "number",
    defaultOperator: "chance",
};

const EXPRESSION_FIELD: TriggerFieldDef = {
    key: EXPRESSION_FIELD_KEY,
    label: "Advanced expression",
    type: "string",
    defaultOperator: "",
};

// The condition fields offered for a trigger: its payload fields + random split
// + the advanced free-form expression escape hatch.
export function triggerConditionFields(triggerEvent: string): TriggerFieldDef[] {
    return [...(TRIGGER_FIELDS[triggerEvent] ?? GENERIC_FIELDS), RANDOM_FIELD, EXPRESSION_FIELD];
}

export function triggerFieldDef(triggerEvent: string, key: string): TriggerFieldDef | undefined {
    return triggerConditionFields(triggerEvent).find((f) => f.key === key);
}

// Build a fresh condition from a picked field key (random -> chance split;
// everything else -> a generic field test with the field's default operator).
export function conditionFromFieldKey(triggerEvent: string, key: string): AutomationCondition {
    if (key === RANDOM_FIELD_KEY) return { field: "random", operator: "chance", value: 50 };
    if (key === EXPRESSION_FIELD_KEY) return { field: "expression", operator: "", expression: "" };
    const def = triggerFieldDef(triggerEvent, key);
    return { field: "field", key, operator: def?.defaultOperator ?? "equals" };
}

// The default condition a new IF node starts with for a given trigger.
export function defaultConditionForTrigger(triggerEvent: string): AutomationCondition {
    const first = triggerConditionFields(triggerEvent)[0];
    return conditionFromFieldKey(triggerEvent, first?.key ?? RANDOM_FIELD_KEY);
}

// The select value representing a condition's chosen field.
export function conditionFieldKey(c: AutomationCondition): string {
    if (c.field === "random") return RANDOM_FIELD_KEY;
    if (c.field === "expression") return EXPRESSION_FIELD_KEY;
    if (c.field === "field") return c.key ?? "";
    return c.field; // legacy
}

export const OPERATOR_LABELS: Record<string, string> = {
    equals: "is",
    not_equals: "is not",
    contains: "contains",
    gte: "≥",
    lte: "≤",
    exists: "is present",
    is_true: "is true",
    chance: "% of the time",
};

// Which operators make sense for each value type.
export function operatorsForType(type: ConditionValueType): { value: string; label: string }[] {
    const ops =
        type === "number"
            ? ["gte", "lte", "equals", "exists"]
            : type === "enum"
              ? ["equals", "not_equals", "exists"]
              : type === "bool"
                ? ["is_true", "exists"]
                : ["equals", "not_equals", "contains", "exists"];
    return ops.map((o) => ({ value: o, label: OPERATOR_LABELS[o] ?? o }));
}

export function defaultOperatorFor(field: string): string {
    // Legacy helper kept for any old callers; new code reads field def operators.
    switch (field) {
        case "confidence":
            return "gte";
        case "has_contact":
            return "is_true";
        case "random":
            return "chance";
        default:
            return "equals";
    }
}

// --- Template variables per trigger (for the insert menus) ------------------
// The keys present in each trigger's event payload, offered as {{.key}} inserts
// (standard Go-template dotted field access) in action message/URL/value fields.
export const TRIGGER_VARIABLES: Record<string, string[]> = {
    "campaign.reply_received": ["contact_email", "contact_id", "campaign_id", "intent", "confidence", "subject", "snippet"],
    "meeting.booked": ["invitee_name", "invitee_email", "event_name", "scheduled_for", "join_url", "source", "contact_id"],
    "meeting.rescheduled": ["invitee_name", "invitee_email", "event_name", "scheduled_for", "join_url", "source", "contact_id"],
    "meeting.canceled": ["invitee_name", "invitee_email", "event_name", "scheduled_for", "source", "contact_id"],
    "campaign.email_bounced": ["contact_email", "campaign_id", "contact_id", "event_type", "provider", "reason"],
    "deliverability.bounce": ["contact_email", "campaign_id", "contact_id", "event_type", "provider", "reason"],
    "deliverability.complaint": ["contact_email", "campaign_id", "contact_id", "event_type", "provider", "reason"],
    "campaign.unsubscribed": ["contact_email", "contact_id", "campaign_id", "source"],
    "warmup.health_changed": ["email", "new_state", "previous_state", "reason"],
    "campaign.action": ["contact_email", "contact_id", "campaign_id", "campaign_name", "first_name", "last_name", "company", "phone"],
    // Inbound webhook payloads are caller-defined, so there are no fixed
    // variables to offer; the user references their own JSON keys directly.
    "inbound.webhook": [],
};

export function triggerVariables(triggerEvent: string): string[] {
    return TRIGGER_VARIABLES[triggerEvent] ?? ["contact_email", "contact_id"];
}

// A representative sample event payload for a trigger, mirroring the backend's
// sampleEventData (internal/app/integration/graph_executor.go). Seeds the test
// panel so a dry run has realistic data to evaluate conditions and render action
// previews against. The user can edit it freely before running.
export function sampleEventData(triggerEvent: string): Record<string, unknown> {
    const base: Record<string, unknown> = {
        contact_email: "jane@example.com",
        contact_id: "00000000-0000-0000-0000-000000000001",
        campaign_id: "00000000-0000-0000-0000-000000000002",
        campaign_name: "Q3 Outbound",
        first_name: "Jane",
        last_name: "Doe",
        company: "Example Inc",
    };
    switch (triggerEvent) {
        case "campaign.reply_received":
            base.intent = "positive";
            base.confidence = 0.92;
            base.subject = "Re: quick question";
            base.snippet = "Sure, let's talk next week.";
            break;
        case "meeting.booked":
        case "meeting.rescheduled":
        case "meeting.canceled":
            base.source = "calendly";
            base.invitee_email = "jane@example.com";
            base.invitee_name = "Jane Doe";
            base.event_name = "Intro call";
            base.scheduled_for = "2026-07-01T15:00:00Z";
            base.join_url = "https://example.com/join/abc";
            break;
        case "campaign.email_bounced":
        case "deliverability.bounce":
        case "deliverability.complaint":
            base.event_type = "bounce";
            base.provider = "ses";
            base.reason = "mailbox full";
            break;
        case "warmup.health_changed":
            base.email = "sender@example.com";
            base.new_state = "watch";
            base.previous_state = "healthy";
            base.reason = "spam placement rising";
            break;
    }
    return base;
}

const prettyKey = (k: string) => k.replace(/_/g, " ");

// A short human summary of a condition, used as the IF node label.
export function conditionLabel(c?: AutomationCondition): string {
    if (!c || !c.field) return "Set a condition";
    // Random split (its own field type).
    if (c.field === "random") return `${Number(c.value ?? 50)}% random`;
    // Advanced free-form expression.
    if (c.field === "expression") {
        const e = (c.expression ?? "").trim();
        if (!e) return "Set an expression";
        return `if ${e.length > 30 ? e.slice(0, 30) + "…" : e}`;
    }
    // Generic field condition.
    if (c.field === "field") {
        const key = c.key ?? "";
        const op = c.operator;
        if (op === "exists") return `${prettyKey(key)} is present`;
        if (op === "is_true") return `${prettyKey(key)} is true`;
        if (key === "confidence") return `confidence ≥ ${Math.round(Number(c.value ?? 0) * 100)}%`;
        const opLbl = OPERATOR_LABELS[op] ?? op;
        const valLbl =
            REPLY_INTENT_OPTIONS.find((o) => o.value === c.value)?.label ??
            WARMUP_STATES.find((o) => o.value === c.value)?.label ??
            String(c.value ?? "…");
        return `${prettyKey(key)} ${opLbl} ${valLbl}`;
    }
    // Legacy semantic fields (older saved automations).
    switch (c.field) {
        case "confidence":
            return `confidence ≥ ${Math.round(Number(c.value ?? 0) * 100)}%`;
        case "has_contact":
            return "has a contact";
        case "intent":
            return `intent is ${REPLY_INTENT_OPTIONS.find((o) => o.value === c.value)?.label ?? String(c.value ?? "…")}`;
        case "source":
            return `source is ${String(c.value ?? "…")}`;
        default:
            return c.field;
    }
}
