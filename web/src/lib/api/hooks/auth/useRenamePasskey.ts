import { useMutation, useQueryClient } from "@tanstack/react-query";
import renamePasskey from "@/lib/api/client/auth/passkey/renameCredential";

export default function useRenamePasskey() {
    const queryClient = useQueryClient();
    return useMutation({
        mutationFn: ({ id, name }: { id: string; name: string }) => renamePasskey(id, name),
        onSuccess: () => {
            queryClient.invalidateQueries({ queryKey: ["passkeys"] });
        },
    });
}
