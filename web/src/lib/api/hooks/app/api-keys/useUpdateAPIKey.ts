import { useMutation, useQueryClient } from "@tanstack/react-query";
import updateAPIKey, { type UpdateAPIKeyInput } from "@/lib/api/client/app/api-keys/updateAPIKey";

export default function useUpdateAPIKey() {
    const queryClient = useQueryClient();

    return useMutation({
        mutationFn: ({ id, data }: { id: string; data: UpdateAPIKeyInput }) => updateAPIKey(id, data),
        onSuccess: () => {
            queryClient.invalidateQueries({ queryKey: ["api-keys"] });
        },
    });
}
