import updateRole from "@/lib/api/client/app/admin/roles/updateRole";
import type Access from "@/lib/api/models/app/admin/Access";
import type Role from "@/lib/api/models/app/admin/Role";
import { useMutation, useQueryClient } from "@tanstack/react-query";

export default function useUpdateRole(id: string) {
    const queryClient = useQueryClient();

    return useMutation({
        mutationFn: (role: Role) => updateRole(id, role),
        onSuccess: (data) => {
            queryClient.setQueryData<Access>(
                ["admin", "roles", "list"],
                (oldData) => {
                    if (!oldData) return oldData;

                    return {
                        ...oldData,
                        roles: oldData.roles.map(r => r.id === id ? data : r)
                    }
                }
            )
        }
    })
}
