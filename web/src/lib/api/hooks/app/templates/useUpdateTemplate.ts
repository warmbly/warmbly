import { useMutation, useQueryClient } from "@tanstack/react-query";
import type { UpdateTemplateInput } from "@/lib/api/models/app/templates/Template";
import updateTemplate from "@/lib/api/client/app/templates/updateTemplate";

export default function useUpdateTemplate() {
    const queryClient = useQueryClient();

    return useMutation({
        mutationFn: ({ id, data }: { id: string; data: UpdateTemplateInput }) => updateTemplate(id, data),
        onSuccess: () => {
            queryClient.invalidateQueries({ queryKey: ["templates"] });
        },
    });
}
