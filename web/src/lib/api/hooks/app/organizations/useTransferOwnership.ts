import { useMutation, useQueryClient } from "@tanstack/react-query";
import transferOwnership from "@/lib/api/client/app/organizations/transferOwnership";

export default function useTransferOwnership() {
    const queryClient = useQueryClient();

    return useMutation({
        mutationFn: (newOwnerUserId: string) => transferOwnership(newOwnerUserId),
        // The workspace list carries the caller's own role, so it is refetched with the roster.
        onSuccess: () =>
            queryClient.invalidateQueries({
                queryKey: ["organizations"],
            }),
    })
}
