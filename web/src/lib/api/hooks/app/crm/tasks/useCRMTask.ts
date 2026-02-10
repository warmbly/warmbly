import { useQuery } from "@tanstack/react-query";
import getCRMTask from "@/lib/api/client/app/crm/tasks/getCRMTask";

export default function useCRMTask(id: string) {
    return useQuery({
        queryKey: ["crm", "tasks", id],
        queryFn: () => getCRMTask(id),
        enabled: !!id,
    })
}
