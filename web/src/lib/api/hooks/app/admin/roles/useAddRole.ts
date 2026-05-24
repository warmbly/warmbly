import addRole from "@/lib/api/client/app/admin/roles/addRole";
import { useMutation } from "@tanstack/react-query";

export default function useAddRole() {
    return useMutation({
        mutationFn: ({ roleId, userId }: { roleId: string, userId: string }) => addRole(userId, roleId),
    })
}
