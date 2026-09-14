import type TaskSelection from "@/lib/api/models/app/crm/TaskSelection";
import type { BulkTasksResponse } from "@/lib/api/models/app/crm/TaskSelection";
import Request from "../../../Request";

// The endpoint also accepts a bare id array; the client always sends the
// selection object so "select all matching" is one request instead of a page
// walk.
export default async function bulkDeleteTasks(selection: TaskSelection): Promise<BulkTasksResponse> {
    return await Request<BulkTasksResponse>({
        method: "DELETE",
        url: "/crm/tasks",
        data: selection,
        authorization: true,
    });
}
