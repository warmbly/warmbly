import updateTag from "@/lib/api/client/app/tags/updateTag";
import type Tag from "@/lib/api/models/app/Tag";
import type User from "@/lib/api/models/auth/User";
import { useMutation, useQueryClient } from "@tanstack/react-query";

export default function useUpdateTag(id: string) {
    const queryClient = useQueryClient();

    return useMutation({
        mutationFn: (tag: Partial<Tag>) => updateTag(id, tag),
        onSuccess: (data) => {
            queryClient.setQueryData<User>(
                ["auth", "me"],
                (oldData) => {
                    if (!oldData) return oldData;

                    return {
                        ...oldData,
                        tags: oldData.tags.map(t => t.id === data.id ? data : t),
                    }
                }
            )
        }
    })
}
