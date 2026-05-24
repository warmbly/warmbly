import { useMutation, useQueryClient } from "@tanstack/react-query";
import updateMemberRole from "@/lib/api/client/app/organizations/updateMemberRole";

export default function useUpdateMemberRole() {
    const queryClient = useQueryClient();

    return useMutation({
        mutationFn: ({ id, data }: { id: string; data: { role: string } }) => updateMemberRole(id, data),
        onSuccess: () => {
            queryClient.invalidateQueries({
                queryKey: ["organizations", "members"],
            })
        }
    })
}
