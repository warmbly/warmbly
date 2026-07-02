import { useMutation, useQueryClient } from "@tanstack/react-query";
import createPipeline from "@/lib/api/client/app/crm/pipelines/createPipeline";
import { useLivePatch } from "@/hooks/useLivePatch";

export default function useCreatePipeline() {
    const queryClient = useQueryClient();
    const { pushPatch } = useLivePatch("crm_pipelines");

    return useMutation({
        mutationFn: (data: { name: string; description?: string }) => createPipeline(data),
        onSuccess: () => {
            queryClient.invalidateQueries({
                queryKey: ["crm", "pipelines", "list"],
            })
            pushPatch({ kind: "pipeline_change" })
        }
    })
}
