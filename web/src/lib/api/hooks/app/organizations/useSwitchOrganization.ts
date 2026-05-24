import { useMutation, useQueryClient } from "@tanstack/react-query";
import switchOrganization from "@/lib/api/client/app/organizations/switchOrganization";

export default function useSwitchOrganization() {
    const queryClient = useQueryClient();

    return useMutation({
        mutationFn: (id: string) => switchOrganization(id),
        onSuccess: () => {
            queryClient.invalidateQueries({
                queryKey: ["organizations", "current"],
            })
        }
    })
}
