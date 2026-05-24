import createCategory from "@/lib/api/client/app/categories/createCategory";
import type User from "@/lib/api/models/auth/User";
import { useMutation, useQueryClient } from "@tanstack/react-query";

export default function useCreateCategory() {
    const queryClient = useQueryClient();

    return useMutation({
        mutationFn: (title: string) => createCategory(title),
        onSuccess: (data) => {
            queryClient.setQueryData<User>(
                ["auth", "me"],
                (oldData) => {
                    if (!oldData) return oldData;

                    return {
                        ...oldData,
                        categories: [
                            ...oldData.categories,
                            data,
                        ]
                    }
                }
            )
        }
    })
}
