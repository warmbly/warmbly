// Typed config for an action sequence node. Mirrors the Go models.ActionConfig.
// Stored in the sequence's `action` jsonb column. Spacing/delays are NOT actions:
// per-step "wait before sending" (wait_after) covers that, so there is no
// standalone wait node.
export type SequenceActionType =
    | "add_tag"
    | "remove_tag"
    | "unsubscribe"
    | "notify"
    | "create_task"
    | "create_deal"
    | "move_deal_stage";

export interface SequenceAction {
    type: SequenceActionType;
    // add_tag / remove_tag — a contact category id
    category_id?: string | null;
    // notify
    notify_event?: string;
    notify_data?: Record<string, unknown>;
    // create_task — open a CRM task for the lead at this step
    task_title?: string;
    task_type?: string; // task type name (user-managed)
    task_priority?: "low" | "medium" | "high" | "urgent";
    task_assigned_to?: string | null;
    task_due_offset_days?: number | null;
    // create_deal / move_deal_stage — CRM deal automation. create_deal opens a
    // new deal for the contact in deal_pipeline_id/deal_stage_id; move_deal_stage
    // moves the contact's most-recent OPEN deal in deal_pipeline_id to
    // deal_stage_id (no-op when the contact has no open deal there).
    deal_pipeline_id?: string;
    deal_stage_id?: string;
    // deal_name supports the same {{.FirstName}}/{{.Company}} templating other
    // campaign copy uses. Only meaningful for create_deal.
    deal_name?: string;
    deal_value?: number;
    deal_currency?: string; // ISO code, defaults to "USD"
}
