import { useMutation, useQueryClient } from "@tanstack/react-query";
import type CRMTask from "@/lib/api/models/app/crm/CRMTask";
import updateCRMTask from "@/lib/api/client/app/crm/tasks/updateCRMTask";

export default function useUpdateCRMTask() {
    const queryClient = useQueryClient();

    return useMutation({
        mutationFn: ({ id, data }: { id: string; data: Partial<CRMTask> }) => updateCRMTask(id, data),
        onSuccess: () => {
            queryClient.invalidateQueries({
                queryKey: ["crm", "tasks"],
            })
        }
    })
}
