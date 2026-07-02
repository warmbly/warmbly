import { useMutation, useQueryClient } from "@tanstack/react-query";
import type { Stage } from "@/lib/api/models/app/crm/Pipeline";
import updateStage from "@/lib/api/client/app/crm/pipelines/updateStage";
import { useLivePatch } from "@/hooks/useLivePatch";

export default function useUpdateStage() {
    const queryClient = useQueryClient();
    // Refresh teammates' pipelines (and their deal boards) instantly on a stage edit.
    const { pushPatch } = useLivePatch("crm_pipelines");

    return useMutation({
        mutationFn: ({ pipelineId, stageId, data }: { pipelineId: string; stageId: string; data: Partial<Stage> }) =>
            updateStage(pipelineId, stageId, data),
        onSuccess: () => {
            queryClient.invalidateQueries({
                queryKey: ["crm", "pipelines"],
            })
            pushPatch({ kind: "pipeline_change" })
        }
    })
}
