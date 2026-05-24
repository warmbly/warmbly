export type CRMTaskPriority = "low" | "medium" | "high" | "urgent";
export type CRMTaskStatus = "pending" | "in_progress" | "completed" | "cancelled";

export default interface CRMTask {
    id: string;
    organization_id: string;
    contact_id?: string;
    deal_id?: string;
    assigned_to?: string;
    created_by: string;
    title: string;
    description?: string;
    due_date?: string;
    priority: CRMTaskPriority;
    status: CRMTaskStatus;
    completed_at?: string;
    created_at: string;
    updated_at: string;
}

export interface CRMTasksResult {
    data: CRMTask[];
    pagination: {
        has_more: boolean;
        next_cursor?: string | null;
    };
}
