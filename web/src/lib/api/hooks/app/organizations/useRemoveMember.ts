import { useMutation, useQueryClient } from "@tanstack/react-query";
import removeMember from "@/lib/api/client/app/organizations/removeMember";

export default function useRemoveMember() {
    const queryClient = useQueryClient();

    return useMutation({
        mutationFn: (id: string) => removeMember(id),
        onSuccess: () => {
            queryClient.invalidateQueries({
                queryKey: ["organizations", "members"],
            })
        }
    })
}
