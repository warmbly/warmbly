import type { SocialChannel } from "@/lib/api/models/app/social/Social";

// Step branching — the routing drawn on the flow canvas. Each branch fires when
// its condition matches (the recipient opened / clicked / replied, or did NOT,
// optionally within N days; or a random split) and routes the contact to a
// target step, or stops the sequence when the target is null. Branches are
// first-match in order; when none match the contact stops unless another
// unconditional connection from the step matches.
//
// Shipped on the sequence PATCH body as `conditions: { branches: [...] }`
// (PATCH /campaigns/:id/sequences/:seqId), so it rides the existing
// useUpdateSequence mutation.

export type BranchField =
    | "opened"
    | "clicked"
    | "replied"
    | "not_opened"
    | "not_clicked"
    | "not_replied"
    | "random"
    // Reply-classification routes. The contact's most-recent inbound reply was
    // classified (see internal/app/replyclassify) and stored on
    // campaign_contact_progress.reply_class. Operator is "ever" (no value).
    // reply_automated == reply_class is auto_reply OR out_of_office.
    | "reply_positive"
    | "reply_negative"
    | "reply_neutral"
    | "reply_automated"
    // The AI step that owns this branch stored this label for the contact
    // (campaign_contact_progress.ai_label). Operator is "is"; the label rides
    // in `label`. Only meaningful on branches out of an AI step.
    | "ai_label"
    // Automatic inbox tagging stored this intent for the contact's human reply
    // (campaign_contact_progress.reply_intent). Operator is "is"; the intent
    // rides in `label`. Empty when tagging is off or the classifier was unsure.
    | "reply_intent"
    // Social signals, read from the contact's social state in this campaign. Connected and replied take
    // "within_days" with a value, or "ever"; replied may name a channel. Invite pending and Open Profile are LinkedIn.
    | "social_connected"
    | "not_social_connected"
    | "social_replied"
    | "not_social_replied"
    | "social_invite_pending"
    | "social_open_profile"
    | "not_social_open_profile";

export const SOCIAL_BRANCH_FIELDS: BranchField[] = [
    "social_replied",
    "not_social_replied",
    "social_connected",
    "not_social_connected",
    "social_invite_pending",
    "social_open_profile",
    "not_social_open_profile",
];

export function isSocialBranchField(field: string): boolean {
    return field.startsWith("social_") || field.startsWith("not_social_");
}

/** The social fields that are a state, not an event, so they take no day window. */
export function isSocialStateField(field: string): boolean {
    return field === "social_invite_pending" || field === "social_open_profile" || field === "not_social_open_profile";
}

export type BranchOperator = "within_days" | "ever" | "chance" | "is";

// The reply-class fields are evaluated with operator "ever" and carry no value.
export const REPLY_BRANCH_FIELDS: BranchField[] = [
    "reply_positive",
    "reply_negative",
    "reply_neutral",
    "reply_automated",
];

export function isReplyBranchField(field: BranchField): boolean {
    return REPLY_BRANCH_FIELDS.includes(field);
}

// Instant-capable fields are the "it happened" signals the backend can react to
// the moment the event lands (a reply is recorded, or an open / click is
// tracked), rather than at the next scheduled step boundary. That is the same
// per-branch Instant toggle the reply paths use, generalized to engagement.
//
// Excluded on purpose: negative signals (not_opened / not_clicked / not_replied)
// and "within N days" windows can only be resolved at a step boundary because
// they assert something did NOT happen in a window; and random / always are not
// event-driven at all. `replied` is also excluded here because it carries a day
// window in this canvas; use the reply_* intent fields for an instant reply path.
export const INSTANT_CAPABLE_FIELDS: BranchField[] = [
    "reply_positive",
    "reply_negative",
    "reply_neutral",
    "reply_automated",
    "reply_intent",
    "opened",
    "clicked",
];

export function isInstantCapableField(field: BranchField): boolean {
    return INSTANT_CAPABLE_FIELDS.includes(field);
}

export interface BranchCondition {
    field: BranchField;
    operator: BranchOperator;
    // Days for `within_days`; percent (1-99) for `random`/`chance`. Omitted for `ever`.
    value?: number;
    // The AI-step label an `ai_label` condition, or the intent a `reply_intent`
    // condition, compares against (operator "is").
    label?: string;
    // Narrows social_replied / not_social_replied to one channel; absent is any channel.
    channel?: SocialChannel;
}

export interface SequenceBranch {
    branch_id: string;
    // The step to route to when this branch matches. null = stop the sequence.
    target_step_id: string | null;
    // ANDed conditions. An empty list is the catch-all "else" branch.
    conditions: BranchCondition[];
    // For an instant-capable branch: whether its action chain fires the moment
    // the signal lands. Absent/true = instant; false = route at the next step
    // boundary. Ignored for fields that are not event-driven.
    instant?: boolean;
}

// The full conditions payload carried on the sequence record + PATCH body.
export interface SequenceConditions {
    branches: SequenceBranch[];
}

export const BRANCH_FIELD_LABELS: Record<BranchField, string> = {
    opened: "opened the email",
    clicked: "clicked a link",
    replied: "replied",
    not_opened: "didn’t open",
    not_clicked: "didn’t click",
    not_replied: "didn’t reply",
    random: "random split",
    reply_positive: "replied: positive",
    reply_negative: "replied: negative",
    reply_neutral: "replied: neutral",
    reply_automated: "auto-reply / out of office",
    ai_label: "AI label is",
    reply_intent: "reply intent is",
    social_replied: "replied on social",
    not_social_replied: "didn’t reply on social",
    social_connected: "accepted the LinkedIn invite",
    not_social_connected: "didn’t accept the LinkedIn invite",
    social_invite_pending: "LinkedIn invite still pending",
    social_open_profile: "has an Open Profile",
    not_social_open_profile: "has no Open Profile",
};

// The intents automatic inbox tagging can store for a human reply, as a
// `reply_intent` condition offers them. Values mirror internal/app/inboxtag.
export const REPLY_INTENTS: { value: string; label: string }[] = [
    { value: "agreed", label: "Agreed" },
    { value: "wants_info", label: "Wants info" },
    { value: "wants_pricing", label: "Wants pricing" },
    { value: "not_now", label: "Not now" },
    { value: "not_interested", label: "Not interested" },
    { value: "wrong_person", label: "Wrong person" },
    { value: "opt_out", label: "Opt out" },
    { value: "scheduling", label: "Scheduling" },
    { value: "in_progress", label: "In progress" },
    { value: "question_answered", label: "Question answered" },
    { value: "unclear", label: "Unclear" },
];

export function replyIntentLabel(intent: string | undefined): string {
    return REPLY_INTENTS.find((i) => i.value === intent)?.label ?? intent ?? "…";
}
