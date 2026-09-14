import { useMutation, useQueryClient } from "@tanstack/react-query";
import bulkDeleteTasks from "@/lib/api/client/app/crm/tasks/bulkDeleteTasks";
import type TaskSelection from "@/lib/api/models/app/crm/TaskSelection";
import { useLivePatch } from "@/hooks/useLivePatch";

export default function useBulkDeleteTasks() {
    const queryClient = useQueryClient();
    const { pushPatch } = useLivePatch("crm_tasks");

    return useMutation({
        mutationFn: (selection: TaskSelection) => bulkDeleteTasks(selection),
        onSuccess: () => {
            queryClient.invalidateQueries({ queryKey: ["crm", "tasks"] });
            pushPatch({ kind: "task_change" });
        },
    });
}
