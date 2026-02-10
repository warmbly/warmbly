import { useMutation, useQueryClient } from "@tanstack/react-query";
import deleteStage from "@/lib/api/client/app/crm/pipelines/deleteStage";

export default function useDeleteStage() {
    const queryClient = useQueryClient();

    return useMutation({
        mutationFn: ({ pipelineId, stageId }: { pipelineId: string; stageId: string }) =>
            deleteStage(pipelineId, stageId),
        onSuccess: () => {
            queryClient.invalidateQueries({
                queryKey: ["crm", "pipelines"],
            })
        }
    })
}
