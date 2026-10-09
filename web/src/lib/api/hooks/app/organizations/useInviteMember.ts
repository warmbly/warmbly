import { useMutation, useQueryClient } from "@tanstack/react-query";
import inviteMember from "@/lib/api/client/app/organizations/inviteMember";

export default function useInviteMember() {
    const queryClient = useQueryClient();

    return useMutation({
        mutationFn: (data: Parameters<typeof inviteMember>[0]) => inviteMember(data),
        onSuccess: () => {
            queryClient.invalidateQueries({
                queryKey: ["organizations", "invitations"],
            })
        }
    })
}
