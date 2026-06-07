import { useInfiniteQuery, type InfiniteData } from "@tanstack/react-query";
import type SearchTasks from "@/lib/api/models/app/crm/SearchTasks";
import type TasksSearchResult from "@/lib/api/models/app/crm/TasksSearchResult";
import searchTasks from "@/lib/api/client/app/crm/tasks/searchTasks";

interface UseSearchTasksProps {
    filters: SearchTasks;
    limit?: number;
    enabled?: boolean;
}

// Server-driven tasks fetch that scales to thousands of rows. Offset-paginated
// (the backend uses offset rather than a keyset cursor so nullable due-date
// sorts don't drop rows), so the page param is the next offset. Pages flatten
// into a single `tasks` list and `total` comes straight off the server so the
// UI can show "N of M loaded".
export default function useSearchTasks({ filters, limit = 50, enabled = true }: UseSearchTasksProps) {
    const queryResult = useInfiniteQuery<
        TasksSearchResult,
        Error,
        InfiniteData<TasksSearchResult, number>,
        [string, string, string, SearchTasks, number],
        number
    >({
        queryKey: ["crm", "tasks", "search", filters, limit],
        queryFn: async ({ pageParam }) => searchTasks(filters, pageParam, limit),
        initialPageParam: 0,
        getNextPageParam: (lastPage) =>
            lastPage.pagination.has_more ? (lastPage.pagination.next_offset ?? undefined) : undefined,
        staleTime: 30_000,
        enabled,
    });

    const tasks = queryResult.data?.pages
        .flatMap((p) => p.data ?? [])
        .filter((t): t is NonNullable<typeof t> => t != null);

    const total = queryResult.data?.pages[0]?.pagination.total ?? 0;

    return { ...queryResult, tasks, total };
}
