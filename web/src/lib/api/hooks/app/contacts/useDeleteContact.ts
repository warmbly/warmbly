import deleteContact from "@/lib/api/client/app/contacts/deleteContact";
import type SearchContactsResult from "@/lib/api/models/app/contacts/SearchContactsResult";
import { useMutation, useQueryClient, type InfiniteData } from "@tanstack/react-query";

export default function useDeleteContact(contact_id: string) {
    const queryClient = useQueryClient();

    return useMutation({
        mutationFn: () => deleteContact(contact_id),
        onSuccess: () => {
            const allLists = queryClient.getQueriesData<InfiniteData<SearchContactsResult>>({
                queryKey: ["campaigns", "list"],
            });

            for (const [key, oldData] of allLists) {
                if (!oldData) continue;

                queryClient.setQueryData(key, {
                    ...oldData,
                    pages: oldData.pages.map((page) => ({
                        ...page,
                        data: page.data.filter((c) => c.id !== contact_id),
                    })),
                });
            }

            queryClient.invalidateQueries({
                queryKey: ["contacts", contact_id]
            });
        }
    })
}
