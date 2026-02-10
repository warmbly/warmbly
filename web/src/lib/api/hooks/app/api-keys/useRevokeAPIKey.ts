import { useMutation, useQueryClient } from "@tanstack/react-query";
import revokeAPIKey from "@/lib/api/client/app/api-keys/revokeAPIKey";

export default function useRevokeAPIKey() {
    const queryClient = useQueryClient();

    return useMutation({
        mutationFn: (id: string) => revokeAPIKey(id),
        onSuccess: () => {
            queryClient.invalidateQueries({
                queryKey: ["api-keys"],
            })
        }
    })
}
