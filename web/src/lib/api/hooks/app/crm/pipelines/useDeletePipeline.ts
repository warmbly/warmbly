import { useMutation, useQueryClient } from "@tanstack/react-query";
import deletePipeline from "@/lib/api/client/app/crm/pipelines/deletePipeline";
import { useLivePatch } from "@/hooks/useLivePatch";

export default function useDeletePipeline() {
    const queryClient = useQueryClient();
    const { pushPatch } = useLivePatch("crm_pipelines");

    return useMutation({
        mutationFn: (id: string) => deletePipeline(id),
        onSuccess: () => {
            queryClient.invalidateQueries({
                queryKey: ["crm", "pipelines"],
            })
            pushPatch({ kind: "pipeline_change" })
        }
    })
}
