import type SearchTasks from "@/lib/api/models/app/crm/SearchTasks";
import type TasksSearchResult from "@/lib/api/models/app/crm/TasksSearchResult";
import Request from "../../../Request";

export default async function searchTasks(
    filters: SearchTasks,
    offset = 0,
    limit = 50,
): Promise<TasksSearchResult> {
    const qs = new URLSearchParams();
    if (offset) qs.set("offset", String(offset));
    if (limit) qs.set("limit", String(limit));
    const suffix = qs.toString() ? `?${qs.toString()}` : "";

    return await Request<TasksSearchResult>({
        method: "POST",
        url: `/crm/tasks/search${suffix}`,
        data: filters,
        authorization: true,
    });
}
