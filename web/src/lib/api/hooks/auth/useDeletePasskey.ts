import { useMutation, useQueryClient } from "@tanstack/react-query";
import deletePasskey from "@/lib/api/client/auth/passkey/deleteCredential";

export default function useDeletePasskey() {
    const queryClient = useQueryClient();
    return useMutation({
        mutationFn: (id: string) => deletePasskey(id),
        onSuccess: () => {
            queryClient.invalidateQueries({ queryKey: ["passkeys"] });
        },
    });
}
