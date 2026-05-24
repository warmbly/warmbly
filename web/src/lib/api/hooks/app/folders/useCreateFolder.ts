import createFolder from "@/lib/api/client/app/folders/createFolder";
import type User from "@/lib/api/models/auth/User";
import { useMutation, useQueryClient } from "@tanstack/react-query";

export default function useCreateFolder() {
    const queryClient = useQueryClient();

    return useMutation({
        mutationFn: (title: string) => createFolder(title),
        onSuccess: (data) => {
            queryClient.setQueryData<User>(
                ["auth", "me"],
                (oldData) => {
                    if (!oldData) return oldData;

                    return {
                        ...oldData,
                        folders: [
                            ...oldData.folders,
                            data,
                        ]
                    }
                }
            )
        }
    })
}
