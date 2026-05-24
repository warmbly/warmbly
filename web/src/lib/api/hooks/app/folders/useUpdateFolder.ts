import updateFolder from "@/lib/api/client/app/folders/updateFolder";
import type Tag from "@/lib/api/models/app/Tag";
import type User from "@/lib/api/models/auth/User";
import { useMutation, useQueryClient } from "@tanstack/react-query";

export default function useUpdateFolder(id: string) {
    const queryClient = useQueryClient();

    return useMutation({
        mutationFn: (tag: Partial<Tag>) => updateFolder(id, tag),
        onSuccess: (data) => {
            queryClient.setQueryData<User>(
                ["auth", "me"],
                (oldData) => {
                    if (!oldData) return oldData;

                    return {
                        ...oldData,
                        folders: oldData.folders.map(t => t.id === data.id ? data : t),
                    }
                }
            )
        }
    })
}
