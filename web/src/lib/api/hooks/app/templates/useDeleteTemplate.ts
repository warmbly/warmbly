import { useMutation, useQueryClient } from "@tanstack/react-query";
import deleteTemplate from "@/lib/api/client/app/templates/deleteTemplate";

export default function useDeleteTemplate() {
    const queryClient = useQueryClient();

    return useMutation({
        mutationFn: (id: string) => deleteTemplate(id),
        onSuccess: () => {
            queryClient.invalidateQueries({
                queryKey: ["templates"],
            })
        }
    })
}
