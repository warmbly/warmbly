import { useMutation, useQueryClient } from "@tanstack/react-query";
import type CRMTask from "@/lib/api/models/app/crm/CRMTask";
import createCRMTask from "@/lib/api/client/app/crm/tasks/createCRMTask";

export default function useCreateCRMTask() {
    const queryClient = useQueryClient();

    return useMutation({
        mutationFn: (data: Partial<CRMTask>) => createCRMTask(data),
        onSuccess: () => {
            queryClient.invalidateQueries({
                queryKey: ["crm", "tasks", "list"],
            })
        }
    })
}
