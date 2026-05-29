import { useMutation, useQueryClient } from "@tanstack/react-query";
import changePlan, {
    type ChangePlanInput,
} from "@/lib/api/client/app/subscription/changePlan";

export default function useChangePlan() {
    const queryClient = useQueryClient();

    return useMutation({
        mutationFn: (data: ChangePlanInput) => changePlan(data),
        onSuccess: () => {
            queryClient.invalidateQueries({
                queryKey: ["subscription"],
            })
        }
    })
}
