import { useMutation, useQueryClient } from "@tanstack/react-query";
import bulkUpdateTasks from "@/lib/api/client/app/crm/tasks/bulkUpdateTasks";
import type { BulkUpdateTasks } from "@/lib/api/models/app/crm/TaskSelection";
import { useLivePatch } from "@/hooks/useLivePatch";

export default function useBulkUpdateTasks() {
    const queryClient = useQueryClient();
    const { pushPatch } = useLivePatch("crm_tasks");

    return useMutation({
        mutationFn: (data: BulkUpdateTasks) => bulkUpdateTasks(data),
        onSuccess: () => {
            queryClient.invalidateQueries({ queryKey: ["crm", "tasks"] });
            pushPatch({ kind: "task_change" });
        },
    });
}
