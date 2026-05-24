import moveFolder from "@/lib/api/client/app/folders/moveFolder";
import type User from "@/lib/api/models/auth/User";
import { useMutation, useQueryClient } from "@tanstack/react-query";

export default function useMoveFolder() {
    const queryClient = useQueryClient();

    return useMutation({
        mutationFn: ({ id, new_position }: { id: string, new_position: number }) => moveFolder(id, new_position),
        onSuccess: (data) => {
            queryClient.setQueryData<User>(
                ["auth", "me"],
                (oldData) => {
                    if (!oldData) return oldData;

                    const folders = oldData.folders.map((t) => {
                        const tpos = data.find(v => v.id === t.id)
                        if (!tpos) {
                            return null
                        }
                        return {
                            ...t,
                            position: tpos.position,
                        }
                    })
                        .filter((t): t is NonNullable<typeof t> => t !== null)
                        .sort((a, b) => a.position - b.position);

                    return {
                        ...oldData,
                        folders,
                    }
                }
            )
        }
    })
}
