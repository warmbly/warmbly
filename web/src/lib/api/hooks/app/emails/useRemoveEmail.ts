import removeEmail from "@/lib/api/client/app/emails/removeEmail";
import type GetEmails from "@/lib/api/models/app/emails/GetEmails";
import { useMutation, useQueryClient, type InfiniteData } from "@tanstack/react-query";

export default function useRemoveEmail(id: string) {
    const queryClient = useQueryClient();

    return useMutation({
        mutationFn: () => removeEmail(id),
        onSuccess: () => {
            const allLists = queryClient.getQueriesData<InfiniteData<GetEmails>>({
                queryKey: ["emails", "list"],
            });

            for (const [key, oldData] of allLists) {
                if (!oldData) continue;

                queryClient.setQueryData(key, {
                    ...oldData,
                    pages: oldData.pages.map((page) => ({
                        ...page,
                        data: page.data.filter((c) => c.id !== id),
                    })),
                });
            }

            queryClient.invalidateQueries({
                queryKey: ["emails", id]
            });
        }
    })
}
