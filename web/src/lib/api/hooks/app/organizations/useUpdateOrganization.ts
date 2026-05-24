import { useMutation, useQueryClient } from "@tanstack/react-query";
import type Organization from "@/lib/api/models/app/organizations/Organization";
import updateOrganization from "@/lib/api/client/app/organizations/updateOrganization";

export default function useUpdateOrganization() {
    const queryClient = useQueryClient();

    return useMutation({
        mutationFn: (data: Partial<Organization>) => updateOrganization(data),
        onSuccess: () => {
            queryClient.invalidateQueries({
                queryKey: ["organizations", "current"],
            })
        }
    })
}
