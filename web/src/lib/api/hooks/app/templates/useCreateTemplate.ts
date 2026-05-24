import { useMutation, useQueryClient } from "@tanstack/react-query";
import createTemplate from "@/lib/api/client/app/templates/createTemplate";

export default function useCreateTemplate() {
    const queryClient = useQueryClient();

    return useMutation({
        mutationFn: (data: { name: string; subject: string; body: string; variables?: string[] }) => createTemplate(data),
        onSuccess: () => {
            queryClient.invalidateQueries({
                queryKey: ["templates", "list"],
            })
        }
    })
}
