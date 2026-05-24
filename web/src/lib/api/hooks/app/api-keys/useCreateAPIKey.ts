import { useMutation, useQueryClient } from "@tanstack/react-query";
import createAPIKey from "@/lib/api/client/app/api-keys/createAPIKey";

export default function useCreateAPIKey() {
    const queryClient = useQueryClient();

    return useMutation({
        mutationFn: (data: { name: string; permissions: string[]; expires_at?: string }) => createAPIKey(data),
        onSuccess: () => {
            queryClient.invalidateQueries({
                queryKey: ["api-keys", "list"],
            })
        }
    })
}
