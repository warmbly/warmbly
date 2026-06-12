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
    | "reply_automated";

export type BranchOperator = "within_days" | "ever" | "chance";

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
};
