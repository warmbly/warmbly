// Typed config for an action sequence node. Mirrors the Go models.ActionConfig.
// Stored in the sequence's `action` jsonb column. Spacing/delays are NOT actions:
// per-step "wait before sending" (wait_after) covers that, so there is no
// standalone wait node.
export type SequenceActionType =
    | "add_tag"
    | "remove_tag"
    | "label_email"
    | "unsubscribe"
    | "create_task"
    | "create_deal"
    | "move_deal_stage"
    | "run_automation"
    | "fire_event";

// One templated input passed to a launched automation (value supports the same
// {{.FirstName}}/{{.Company}} contact templating campaign copy uses).
export interface ActionKV {
    key: string;
    value: string;
}

export interface SequenceAction {
    type: SequenceActionType;
    // add_tag / remove_tag — a contact category id
    category_id?: string | null;
    // label_email — unibox conversation labels (same category registry as tags)
    // applied to the thread the contact replied on. Reply-branch only; a no-op
    // when the contact has not replied.
    label_ids?: string[];
    // create_task — open a CRM task for the lead at this step
    task_title?: string;
    task_type?: string; // task type name (user-managed)
    task_priority?: "low" | "medium" | "high" | "urgent";
    task_assigned_to?: string | null;
    task_assigned_team_id?: string | null;
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
    // run_automation — launch an automation when the lead reaches this step,
    // passing templated key/value inputs as the automation's event data.
    automation_id?: string | null;
    automation_values?: ActionKV[];
    // fire_event — publish a custom event to the realtime gateway; subscribers
    // receive it over the API websocket (no public URL). event_name + each field
    // value are templated against the contact; the fields become the payload.
    event_name?: string;
    event_fields?: ActionKV[];
}
