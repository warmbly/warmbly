import type { BulkTasksResponse, BulkUpdateTasks } from "@/lib/api/models/app/crm/TaskSelection";
import Request from "../../../Request";

export default async function bulkUpdateTasks(data: BulkUpdateTasks): Promise<BulkTasksResponse> {
    return await Request<BulkTasksResponse>({
        method: "PATCH",
        url: "/crm/tasks",
        data,
        authorization: true,
    });
}
