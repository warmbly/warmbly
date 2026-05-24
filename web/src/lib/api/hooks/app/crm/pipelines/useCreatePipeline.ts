import { useMutation, useQueryClient } from "@tanstack/react-query";
import createPipeline from "@/lib/api/client/app/crm/pipelines/createPipeline";

export default function useCreatePipeline() {
    const queryClient = useQueryClient();

    return useMutation({
        mutationFn: (data: { name: string; description?: string }) => createPipeline(data),
        onSuccess: () => {
            queryClient.invalidateQueries({
                queryKey: ["crm", "pipelines", "list"],
            })
        }
    })
}
