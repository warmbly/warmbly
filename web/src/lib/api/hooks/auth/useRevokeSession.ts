import { useMutation, useQueryClient } from "@tanstack/react-query";
import revokeSession from "@/lib/api/client/auth/sessions/revokeSession";

export default function useRevokeSession() {
    const queryClient = useQueryClient();
    return useMutation({
        mutationFn: (id: string) => revokeSession(id),
        onSuccess: () => {
            queryClient.invalidateQueries({ queryKey: ["sessions"] });
        },
    });
}
