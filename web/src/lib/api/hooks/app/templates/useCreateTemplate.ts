import { useMutation, useQueryClient } from "@tanstack/react-query";
import type { CreateTemplateInput } from "@/lib/api/models/app/templates/Template";
import createTemplate from "@/lib/api/client/app/templates/createTemplate";

export default function useCreateTemplate() {
    const queryClient = useQueryClient();

    return useMutation({
        mutationFn: (data: CreateTemplateInput) => createTemplate(data),
        onSuccess: () => {
            queryClient.invalidateQueries({ queryKey: ["templates"] });
        },
    });
}
