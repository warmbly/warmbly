import { useMutation, useQueryClient } from "@tanstack/react-query";
import deleteCRMTask from "@/lib/api/client/app/crm/tasks/deleteCRMTask";
import { useLivePatch } from "@/hooks/useLivePatch";

export default function useDeleteCRMTask() {
    const queryClient = useQueryClient();
    const { pushPatch } = useLivePatch("crm_tasks");

    return useMutation({
        mutationFn: (id: string) => deleteCRMTask(id),
        onSuccess: () => {
            queryClient.invalidateQueries({
                queryKey: ["crm", "tasks"],
            })
            pushPatch({ kind: "task_change" })
        }
    })
}
