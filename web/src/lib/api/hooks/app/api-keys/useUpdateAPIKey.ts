import { useMutation, useQueryClient } from "@tanstack/react-query";
import type APIKey from "@/lib/api/models/app/apikeys/APIKey";
import updateAPIKey from "@/lib/api/client/app/api-keys/updateAPIKey";

export default function useUpdateAPIKey() {
    const queryClient = useQueryClient();

    return useMutation({
        mutationFn: ({ id, data }: { id: string; data: Partial<APIKey> }) => updateAPIKey(id, data),
        onSuccess: () => {
            queryClient.invalidateQueries({
                queryKey: ["api-keys"],
            })
        }
    })
}
