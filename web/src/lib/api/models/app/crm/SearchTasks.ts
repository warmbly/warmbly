// Filter body for POST /crm/tasks/search and /crm/tasks/summary. Every field
// is optional; an empty body matches every task in the org (the "All tasks"
// default). The same body drives the rows and the summary totals, so a header
// number always reflects the exact filter shown below it.

import type { CRMTaskPriority, CRMTaskStatus } from "@/lib/api/models/app/crm/CRMTask";

export type TaskSortBy =
    | "created_at"
    | "updated_at"
    | "due_date"
    | "priority"
    | "title";

export default interface SearchTasks {
    query: string;
    statuses: CRMTaskStatus[];
    priorities: CRMTaskPriority[];
    // Task type NAMEs (crm_tasks.type), e.g. ["Call", "Email"].
    types: string[];
    // User UUIDs (assigned_to). String-matched on the server.
    assigned_to: string[];
    contact_id?: string;
    deal_id?: string;
    due_after?: string;
    due_before?: string;
    // due_date < now() AND status NOT IN ('completed','cancelled').
    overdue?: boolean;
    sort_by: TaskSortBy;
    // false => DESC (default), true => ASC.
    reverse: boolean;
}

export const EMPTY_TASK_SEARCH: SearchTasks = {
    query: "",
    statuses: [],
    priorities: [],
    types: [],
    assigned_to: [],
    sort_by: "created_at",
    reverse: false,
};
