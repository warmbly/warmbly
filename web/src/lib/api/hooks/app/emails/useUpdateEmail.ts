import updateEmail from "@/lib/api/client/app/emails/updateEmail";
import type GetEmails from "@/lib/api/models/app/emails/GetEmails";
import type Inbox from "@/lib/api/models/app/emails/Inbox";
import { useMutation, useQueryClient, type InfiniteData } from "@tanstack/react-query";

export default function useUpdateEmail(id: string) {
    const queryClient = useQueryClient();

    return useMutation({
        mutationFn: (inbox: Partial<Inbox>) => updateEmail(id, inbox),
        onSuccess: (data) => {
            const allLists = queryClient.getQueriesData<InfiniteData<GetEmails>>({
                queryKey: ["emails", "list"],
            });

            for (const [key, oldData] of allLists) {
                if (!oldData) continue;

                queryClient.setQueryData(key, {
                    ...oldData,
                    pages: oldData.pages.map((page) => ({
                        ...page,
                        data: page.data.map((c) => c.id === id ? data : c),
                    })),
                });
            }

            queryClient.setQueryData<Inbox>(
                ["emails", id],
                data
            );
        }
    })
}
