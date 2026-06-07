import type SearchTasks from "@/lib/api/models/app/crm/SearchTasks";
import type { TasksSummary } from "@/lib/api/models/app/crm/TasksSearchResult";
import Request from "../../../Request";

export default async function tasksSummary(filters: SearchTasks): Promise<TasksSummary> {
    return await Request<TasksSummary>({
        method: "POST",
        url: "/crm/tasks/summary",
        data: filters,
        authorization: true,
    });
}
