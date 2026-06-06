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
    | "random";

export type BranchOperator = "within_days" | "ever" | "chance";

export interface BranchCondition {
    field: BranchField;
    operator: BranchOperator;
    // Days for `within_days`; percent (1-99) for `random`/`chance`. Omitted for `ever`.
    value?: number;
}

export interface SequenceBranch {
    branch_id: string;
    // The step to route to when this branch matches. null = stop the sequence.
    target_sequence_id: string | null;
    // ANDed conditions. An empty list is the catch-all "else" branch.
    conditions: BranchCondition[];
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
};
