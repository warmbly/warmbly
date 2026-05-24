// One pending soft-delete on the backend. Returned as
// `pending_deletion` inside `DangerZoneStatus`.
//
// Lifecycle: pending -> (executing -> completed) or (cancelled).
// The UI only ever sees `pending` here — the others are bookkeeping.

export type DeletionResourceType = "organization" | "user";

export type DeletionStatus =
    | "pending"
    | "executing"
    | "completed"
    | "cancelled"
    | "failed";

export default interface ScheduledDeletion {
    id: string;

    resource_type: DeletionResourceType;
    resource_id: string;

    organization_id?: string;

    requested_by_user_id: string;
    reason?: string;

    scheduled_at: Date;
    execute_after: Date;
    grace_days: number;

    status: DeletionStatus;

    cancelled_at?: Date;
    cancelled_by_user_id?: string;
    cancelled_reason?: string;

    executed_at?: Date;
    execution_error?: string;

    last_reminder_at?: Date;
}
