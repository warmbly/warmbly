import deleteTag from "@/lib/api/client/app/tags/deleteTag";
import type User from "@/lib/api/models/auth/User";
import { useMutation, useQueryClient } from "@tanstack/react-query";

export default function useDeleteTag(id: string) {
    const queryClient = useQueryClient();

    return useMutation({
        mutationFn: () => deleteTag(id),
        onSuccess: () => {
            queryClient.setQueryData<User>(
                ["auth", "me"],
                (oldData) => {
                    if (!oldData) return oldData;

                    return {
                        ...oldData,
                        tags: oldData.tags.filter(t => t.id !== id)
                    }
                }
            )
        }
    })
}
