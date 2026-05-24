import deleteFolder from "@/lib/api/client/app/folders/deleteFolder";
import type User from "@/lib/api/models/auth/User";
import { useMutation, useQueryClient } from "@tanstack/react-query";

export default function useDeleteFolder(id: string) {
    const queryClient = useQueryClient();

    return useMutation({
        mutationFn: () => deleteFolder(id),
        onSuccess: () => {
            queryClient.setQueryData<User>(
                ["auth", "me"],
                (oldData) => {
                    if (!oldData) return oldData;

                    return {
                        ...oldData,
                        folders: oldData.folders.filter(t => t.id !== id)
                    }
                }
            )
        }
    })
}
