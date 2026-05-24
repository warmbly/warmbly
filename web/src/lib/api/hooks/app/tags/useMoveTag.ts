import moveTag from "@/lib/api/client/app/tags/moveTag";
import type User from "@/lib/api/models/auth/User";
import { useMutation, useQueryClient } from "@tanstack/react-query";

export default function useMoveTag() {
    const queryClient = useQueryClient();

    return useMutation({
        mutationFn: ({ id, new_position }: { id: string, new_position: number }) => moveTag(id, new_position),
        onSuccess: (data) => {
            queryClient.setQueryData<User>(
                ["auth", "me"],
                (oldData) => {
                    if (!oldData) return oldData;

                    const tags = oldData.tags.map((t) => {
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
                        tags,
                    }
                }
            )
        }
    })
}
