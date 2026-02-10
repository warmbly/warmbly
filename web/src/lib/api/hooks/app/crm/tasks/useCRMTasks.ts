import { useQuery } from "@tanstack/react-query";
import listCRMTasks from "@/lib/api/client/app/crm/tasks/listCRMTasks";

export default function useCRMTasks() {
    return useQuery({
        queryKey: ["crm", "tasks", "list"],
        queryFn: () => listCRMTasks(),
    })
}
