import removeRole from "@/lib/api/client/app/admin/roles/removeRole";
import { useMutation } from "@tanstack/react-query";

export default function useAddRole() {
    return useMutation({
        mutationFn: ({ roleId, userId }: { roleId: string, userId: string }) => removeRole(userId, roleId),
    })
}
