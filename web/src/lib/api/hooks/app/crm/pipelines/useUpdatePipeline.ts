import { useMutation, useQueryClient } from "@tanstack/react-query";
import type Pipeline from "@/lib/api/models/app/crm/Pipeline";
import updatePipeline from "@/lib/api/client/app/crm/pipelines/updatePipeline";
import { useLivePatch } from "@/hooks/useLivePatch";

export default function useUpdatePipeline() {
    const queryClient = useQueryClient();
    const { pushPatch } = useLivePatch("crm_pipelines");

    return useMutation({
        mutationFn: ({ id, data }: { id: string; data: Partial<Pipeline> }) => updatePipeline(id, data),
        onSuccess: () => {
            queryClient.invalidateQueries({
                queryKey: ["crm", "pipelines"],
            })
            pushPatch({ kind: "pipeline_change" })
        }
    })
}
