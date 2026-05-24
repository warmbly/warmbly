import updateCategory from "@/lib/api/client/app/categories/updateCategory";
import type Tag from "@/lib/api/models/app/Tag";
import type User from "@/lib/api/models/auth/User";
import { useMutation, useQueryClient } from "@tanstack/react-query";

export default function useUpdateCategory(id: string) {
    const queryClient = useQueryClient();

    return useMutation({
        mutationFn: (tag: Partial<Tag>) => updateCategory(id, tag),
        onSuccess: (data) => {
            queryClient.setQueryData<User>(
                ["auth", "me"],
                (oldData) => {
                    if (!oldData) return oldData;

                    return {
                        ...oldData,
                        categories: oldData.categories.map(t => t.id === data.id ? data : t),
                    }
                }
            )
        }
    })
}
