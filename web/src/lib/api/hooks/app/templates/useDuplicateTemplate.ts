import { useMutation, useQueryClient } from "@tanstack/react-query";
import duplicateTemplate from "@/lib/api/client/app/templates/duplicateTemplate";

export default function useDuplicateTemplate() {
    const queryClient = useQueryClient();

    return useMutation({
        mutationFn: (id: string) => duplicateTemplate(id),
        onSuccess: () => {
            queryClient.invalidateQueries({ queryKey: ["templates"] });
        },
    });
}
