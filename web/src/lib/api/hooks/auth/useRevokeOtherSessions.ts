import { useMutation, useQueryClient } from "@tanstack/react-query";
import revokeOtherSessions from "@/lib/api/client/auth/sessions/revokeOtherSessions";

export default function useRevokeOtherSessions() {
    const queryClient = useQueryClient();
    return useMutation({
        mutationFn: () => revokeOtherSessions(),
        onSuccess: () => {
            queryClient.invalidateQueries({ queryKey: ["sessions"] });
        },
    });
}
