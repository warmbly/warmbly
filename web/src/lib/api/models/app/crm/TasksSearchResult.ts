import type CRMTask from "@/lib/api/models/app/crm/CRMTask";

export interface TasksSearchPagination {
    total: number;
    limit: number;
    offset: number;
    has_more: boolean;
    next_offset?: number | null;
}

export default interface TasksSearchResult {
    data: CRMTask[];
    pagination: TasksSearchPagination;
}

// Server-aggregated totals over a SearchTasks filter. Every number here is a
// COUNT over the whole matching set — never a client reduce over a loaded
// page — so the header stats stay honest at scale.
export interface TasksSummary {
    total: number;
    pending_count: number;
    in_progress_count: number;
    completed_count: number;
    cancelled_count: number;
    // due_date < now() AND status NOT IN ('completed','cancelled').
    overdue_count: number;
    // priority IN ('high','urgent').
    high_priority_count: number;
}
