// Typed config for an action sequence node. Mirrors the Go models.ActionConfig.
// Stored in the sequence's `action` jsonb column. Spacing/delays are NOT actions:
// per-step "wait before sending" (wait_after) covers that, so there is no
// standalone wait node.
export type SequenceActionType = "add_tag" | "remove_tag" | "unsubscribe" | "notify";

export interface SequenceAction {
    type: SequenceActionType;
    // add_tag / remove_tag — a contact category id
    category_id?: string | null;
    // notify
    notify_event?: string;
    notify_data?: Record<string, unknown>;
}
