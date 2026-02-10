import { useMutation, useQueryClient } from "@tanstack/react-query";
import deletePipeline from "@/lib/api/client/app/crm/pipelines/deletePipeline";

export default function useDeletePipeline() {
    const queryClient = useQueryClient();

    return useMutation({
        mutationFn: (id: string) => deletePipeline(id),
        onSuccess: () => {
            queryClient.invalidateQueries({
                queryKey: ["crm", "pipelines"],
            })
        }
    })
}
