import { useMutation, useQueryClient } from "@tanstack/react-query";
import createStage from "@/lib/api/client/app/crm/pipelines/createStage";
import { useLivePatch } from "@/hooks/useLivePatch";

export default function useCreateStage() {
    const queryClient = useQueryClient();
    const { pushPatch } = useLivePatch("crm_pipelines");

    return useMutation({
        mutationFn: ({ pipelineId, data }: { pipelineId: string; data: { name: string; position: number; color?: string } }) =>
            createStage(pipelineId, data),
        onSuccess: () => {
            queryClient.invalidateQueries({
                queryKey: ["crm", "pipelines"],
            })
            pushPatch({ kind: "pipeline_change" })
        }
    })
}
