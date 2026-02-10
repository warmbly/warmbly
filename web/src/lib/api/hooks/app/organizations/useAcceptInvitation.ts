import { useMutation, useQueryClient } from "@tanstack/react-query";
import acceptInvitation from "@/lib/api/client/app/organizations/acceptInvitation";

export default function useAcceptInvitation() {
    const queryClient = useQueryClient();

    return useMutation({
        mutationFn: (data: { invitation_id: string }) => acceptInvitation(data),
        onSuccess: () => {
            queryClient.invalidateQueries({
                queryKey: ["invitations", "mine"],
            })
            queryClient.invalidateQueries({
                queryKey: ["organizations", "list"],
            })
        }
    })
}
