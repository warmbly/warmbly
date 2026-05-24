import { useMutation, useQueryClient } from "@tanstack/react-query";
import reorderTemplates from "@/lib/api/client/app/templates/reorderTemplates";

export default function useReorderTemplates() {
    const queryClient = useQueryClient();

    return useMutation({
        mutationFn: (ids: string[]) => reorderTemplates(ids),
        onSuccess: () => {
            queryClient.invalidateQueries({ queryKey: ["templates"] });
        },
    });
}
