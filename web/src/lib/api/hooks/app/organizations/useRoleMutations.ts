import { useMutation, useQueryClient } from "@tanstack/react-query";
import createRole from "@/lib/api/client/app/organizations/createRole";
import updateRole from "@/lib/api/client/app/organizations/updateRole";
import deleteRole from "@/lib/api/client/app/organizations/deleteRole";

// Role edits write through to assigned members server-side, so the member
// roster must refresh alongside the role list.
const invalidate = (qc: ReturnType<typeof useQueryClient>) => {
    void qc.invalidateQueries({ queryKey: ["organizations", "roles"] });
    void qc.invalidateQueries({ queryKey: ["organizations", "members"] });
};

export function useCreateRole() {
    const queryClient = useQueryClient();
    return useMutation({
        mutationFn: (data: { name: string; description?: string; color?: string; permissions: number }) => createRole(data),
        onSuccess: () => invalidate(queryClient),
    })
}

export function useUpdateRole() {
    const queryClient = useQueryClient();
    return useMutation({
        mutationFn: ({ id, data }: { id: string; data: { name?: string; description?: string; color?: string; permissions?: number } }) => updateRole(id, data),
        onSuccess: () => invalidate(queryClient),
    })
}

export function useDeleteRole() {
    const queryClient = useQueryClient();
    return useMutation({
        mutationFn: (id: string) => deleteRole(id),
        onSuccess: () => invalidate(queryClient),
    })
}
