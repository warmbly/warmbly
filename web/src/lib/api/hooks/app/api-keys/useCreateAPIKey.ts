import { useMutation, useQueryClient } from "@tanstack/react-query";
import createAPIKey, { type CreateAPIKeyInput } from "@/lib/api/client/app/api-keys/createAPIKey";

export default function useCreateAPIKey() {
    const queryClient = useQueryClient();

    return useMutation({
        mutationFn: (data: CreateAPIKeyInput) => createAPIKey(data),
        onSuccess: () => {
            queryClient.invalidateQueries({ queryKey: ["api-keys"] });
        },
    });
}
