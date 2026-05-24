import { useMutation, useQueryClient } from "@tanstack/react-query";
import transferOwnership from "@/lib/api/client/app/organizations/transferOwnership";

export default function useTransferOwnership() {
    const queryClient = useQueryClient();

    return useMutation({
        mutationFn: (data: { user_id: string }) => transferOwnership(data),
        onSuccess: () => {
            queryClient.invalidateQueries({
                queryKey: ["organizations", "members"],
            })
            queryClient.invalidateQueries({
                queryKey: ["organizations", "current"],
            })
        }
    })
}
