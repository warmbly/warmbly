import { useMutation, useQueryClient } from "@tanstack/react-query";
import type CRMTask from "@/lib/api/models/app/crm/CRMTask";
import updateCRMTask from "@/lib/api/client/app/crm/tasks/updateCRMTask";
import { useLivePatch } from "@/hooks/useLivePatch";

export default function useUpdateCRMTask() {
    const queryClient = useQueryClient();
    // Nudge teammates on the tasks view to refresh instantly (e.g. a status
    // toggle), ahead of the durable audit refetch. Send-only (no subscription).
    const { pushPatch } = useLivePatch("crm_tasks");

    return useMutation({
        mutationFn: ({ id, data }: { id: string; data: Partial<CRMTask> }) => updateCRMTask(id, data),
        onSuccess: () => {
            queryClient.invalidateQueries({
                queryKey: ["crm", "tasks"],
            })
            pushPatch({ kind: "task_change" })
        }
    })
}
