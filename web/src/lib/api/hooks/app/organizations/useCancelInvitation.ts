import { useMutation, useQueryClient } from "@tanstack/react-query";
import cancelInvitation from "@/lib/api/client/app/organizations/cancelInvitation";

export default function useCancelInvitation() {
    const queryClient = useQueryClient();

    return useMutation({
        mutationFn: (id: string) => cancelInvitation(id),
        onSuccess: () => {
            queryClient.invalidateQueries({
                queryKey: ["organizations", "invitations"],
            })
        }
    })
}
