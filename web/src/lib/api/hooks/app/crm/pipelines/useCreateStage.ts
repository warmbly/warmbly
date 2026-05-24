import { useMutation, useQueryClient } from "@tanstack/react-query";
import createStage from "@/lib/api/client/app/crm/pipelines/createStage";

export default function useCreateStage() {
    const queryClient = useQueryClient();

    return useMutation({
        mutationFn: ({ pipelineId, data }: { pipelineId: string; data: { name: string; position: number; color?: string } }) =>
            createStage(pipelineId, data),
        onSuccess: () => {
            queryClient.invalidateQueries({
                queryKey: ["crm", "pipelines"],
            })
        }
    })
}
