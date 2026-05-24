import deleteRole from "@/lib/api/client/app/admin/roles/deleteRole";
import type Access from "@/lib/api/models/app/admin/Access";
import { useMutation, useQueryClient } from "@tanstack/react-query";

export default function useDeleteRole(id: string) {
    const queryClient = useQueryClient();

    return useMutation({
        mutationFn: () => deleteRole(id),
        onSuccess: () => {
            queryClient.setQueryData<Access>(
                ["admin", "roles", "list"],
                (oldData) => {
                    if (!oldData) return oldData;

                    return {
                        ...oldData,
                        roles: oldData.roles.filter(r => r.id != id)
                    }
                }
            )
        }
    })
}
