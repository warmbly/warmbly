import { useMutation, useQueryClient } from "@tanstack/react-query";
import type { Stage } from "@/lib/api/models/app/crm/Pipeline";
import updateStage from "@/lib/api/client/app/crm/pipelines/updateStage";

export default function useUpdateStage() {
    const queryClient = useQueryClient();

    return useMutation({
        mutationFn: ({ pipelineId, stageId, data }: { pipelineId: string; stageId: string; data: Partial<Stage> }) =>
            updateStage(pipelineId, stageId, data),
        onSuccess: () => {
            queryClient.invalidateQueries({
                queryKey: ["crm", "pipelines"],
            })
        }
    })
}
