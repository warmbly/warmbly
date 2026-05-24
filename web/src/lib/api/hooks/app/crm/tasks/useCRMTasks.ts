import { useQuery } from "@tanstack/react-query";
import listCRMTasks, { type ListCRMTasksParams } from "@/lib/api/client/app/crm/tasks/listCRMTasks";

export default function useCRMTasks(params: ListCRMTasksParams = {}) {
    return useQuery({
        queryKey: ["crm", "tasks", "list", params],
        queryFn: () => listCRMTasks(params),
        staleTime: 30_000,
    });
}
