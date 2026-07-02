import { useMutation, useQueryClient } from "@tanstack/react-query";
import deleteStage from "@/lib/api/client/app/crm/pipelines/deleteStage";
import { useLivePatch } from "@/hooks/useLivePatch";

export default function useDeleteStage() {
    const queryClient = useQueryClient();
    const { pushPatch } = useLivePatch("crm_pipelines");

    return useMutation({
        mutationFn: ({ pipelineId, stageId }: { pipelineId: string; stageId: string }) =>
            deleteStage(pipelineId, stageId),
        onSuccess: () => {
            queryClient.invalidateQueries({
                queryKey: ["crm", "pipelines"],
            })
            pushPatch({ kind: "pipeline_change" })
        }
    })
}
