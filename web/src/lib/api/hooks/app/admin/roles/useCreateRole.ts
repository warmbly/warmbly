import createRole from "@/lib/api/client/app/admin/roles/createRole";
import type Access from "@/lib/api/models/app/admin/Access";
import { useMutation, useQueryClient } from "@tanstack/react-query";

export default function useCreateRole() {
    const queryClient = useQueryClient();

    return useMutation({
        mutationFn: (title: string) => createRole(title),
        onSuccess: (data) => {
            queryClient.setQueryData<Access>(
                ["admin", "roles", "list"],
                (oldData) => {
                    if (!oldData) return oldData;

                    return {
                        ...oldData,
                        roles: [
                            ...oldData.roles,
                            data,
                        ]
                    }
                }
            )
        }
    })
}
