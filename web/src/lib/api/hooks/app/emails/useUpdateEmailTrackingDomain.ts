import updateEmailTrackingDomain from "@/lib/api/client/app/emails/updateEmailTrackingDomain";
import type GetEmails from "@/lib/api/models/app/emails/GetEmails";
import type Inbox from "@/lib/api/models/app/emails/Inbox";
import { useMutation, useQueryClient, type InfiniteData } from "@tanstack/react-query";

export default function useUpdateEmail(id: string) {
    const queryClient = useQueryClient();

    return useMutation({
        mutationFn: (tracking_domain: string) => updateEmailTrackingDomain(id, tracking_domain),
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
                        data: page.data.map((c) => c.id === id ? {
                            ...c,
                            tracking_domain: data.tracking_domain,
                        } : c),
                    })),
                });
            }

            queryClient.setQueryData<Inbox>(
                ["emails", id],
                (oldData) => {
                    if (!oldData) return oldData;

                    return {
                        ...oldData,
                        tracking_domain: data.tracking_domain,
                    }
                }
            );
        }
    })
}
