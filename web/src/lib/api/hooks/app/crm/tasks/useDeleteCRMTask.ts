import { useMutation, useQueryClient } from "@tanstack/react-query";
import deleteCRMTask from "@/lib/api/client/app/crm/tasks/deleteCRMTask";

export default function useDeleteCRMTask() {
    const queryClient = useQueryClient();

    return useMutation({
        mutationFn: (id: string) => deleteCRMTask(id),
        onSuccess: () => {
            queryClient.invalidateQueries({
                queryKey: ["crm", "tasks"],
            })
        }
    })
}
