import deleteCategory from "@/lib/api/client/app/categories/deleteCategory";
import type User from "@/lib/api/models/auth/User";
import { useMutation, useQueryClient } from "@tanstack/react-query";

export default function useDeleteCategory(id: string) {
    const queryClient = useQueryClient();

    return useMutation({
        mutationFn: () => deleteCategory(id),
        onSuccess: () => {
            queryClient.setQueryData<User>(
                ["auth", "me"],
                (oldData) => {
                    if (!oldData) return oldData;

                    return {
                        ...oldData,
                        categories: oldData.categories.filter(t => t.id !== id)
                    }
                }
            )
        }
    })
}
