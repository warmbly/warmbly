// Response of GET /organization/current/danger-zone and GET /me/danger-zone.
//
// Always returns the metadata the dialog needs (name to display, the
// phrase the user has to type, grace days). `pending_deletion` is only
// present when the resource is currently scheduled for hard delete.

import type ScheduledDeletion from "./ScheduledDeletion";
import type { DeletionResourceType } from "./ScheduledDeletion";

export default interface DangerZoneStatus {
    resource_type: DeletionResourceType;
    resource_id: string;
    resource_name: string;
    confirmation_hint: string;
    grace_days: number;
    pending_deletion?: ScheduledDeletion;
}
