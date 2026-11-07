import { useMutation, useQueryClient } from "@tanstack/react-query";
import unlinkIdentity from "@/lib/api/client/auth/identities/unlink";

// Not optimistic: the server refuses removing the last way to sign in.
export default function useUnlinkIdentity() {
    const queryClient = useQueryClient();
    return useMutation({
        mutationFn: (provider: string) => unlinkIdentity(provider),
        onSettled: () => {
            queryClient.invalidateQueries({ queryKey: ["auth", "identities"] });
            queryClient.invalidateQueries({ queryKey: ["auth", "reauth-methods"] });
            queryClient.invalidateQueries({ queryKey: ["sessions"] });
        },
    });
}
