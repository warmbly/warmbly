import createTag from "@/lib/api/client/app/tags/createTag";
import type User from "@/lib/api/models/auth/User";
import { useMutation, useQueryClient } from "@tanstack/react-query";

export default function useCreateTag() {
    const queryClient = useQueryClient();

    return useMutation({
        mutationFn: (title: string) => createTag(title),
        onSuccess: (data) => {
            queryClient.setQueryData<User>(
                ["auth", "me"],
                (oldData) => {
                    if (!oldData) return oldData;

                    return {
                        ...oldData,
                        tags: [
                            ...oldData.tags,
                            data,
                        ]
                    }
                }
            )
        }
    })
}
