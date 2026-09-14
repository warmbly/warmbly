import type SearchTasks from "./SearchTasks";
import type { CRMTaskPriority, CRMTaskStatus } from "./CRMTask";

// The tasks a bulk action applies to. Either an explicit list of ticked ids,
// or "select all matching": the same search the list ran, minus the rows
// unticked after it. The filter form is what lets an action reach past the
// pages the table has loaded.
export default interface TaskSelection {
    tasks: string[];
    all?: boolean;
    filters?: SearchTasks;
    exclude?: string[];
}

// PATCH /crm/tasks: a selection plus the fields to write on every task in it.
export interface BulkUpdateTasks extends TaskSelection {
    status?: CRMTaskStatus;
    priority?: CRMTaskPriority;
}

export interface BulkTasksResponse {
    affected: number;
}
