import { useMutation, useQueryClient } from "@tanstack/react-query";
import inviteMember from "@/lib/api/client/app/organizations/inviteMember";

export default function useInviteMember() {
    const queryClient = useQueryClient();

    return useMutation({
        mutationFn: (data: { email: string; role: string }) => inviteMember(data),
        onSuccess: () => {
            queryClient.invalidateQueries({
                queryKey: ["organizations", "invitations"],
            })
        }
    })
}
