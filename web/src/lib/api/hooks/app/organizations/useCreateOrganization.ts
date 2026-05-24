import { useMutation, useQueryClient } from "@tanstack/react-query";
import createOrganization from "@/lib/api/client/app/organizations/createOrganization";

export default function useCreateOrganization() {
    const queryClient = useQueryClient();

    return useMutation({
        mutationFn: (data: { name: string }) => createOrganization(data),
        onSuccess: () => {
            queryClient.invalidateQueries({
                queryKey: ["organizations", "list"],
            })
        }
    })
}
