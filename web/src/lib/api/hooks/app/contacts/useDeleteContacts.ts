import deleteContacts from "@/lib/api/client/app/contacts/deleteContacts";
import type SearchContactsResult from "@/lib/api/models/app/contacts/SearchContactsResult";
import { useMutation, useQueryClient, type InfiniteData } from "@tanstack/react-query";

export default function useDeleteContacts() {
    const queryClient = useQueryClient();

    return useMutation({
        mutationFn: (contact_ids: string[]) => deleteContacts(contact_ids),
        onSuccess: (_, contact_ids) => {
            const allLists = queryClient.getQueriesData<InfiniteData<SearchContactsResult>>({
                queryKey: ["campaigns", "list"],
            });

            for (const [key, oldData] of allLists) {
                if (!oldData) continue;

                queryClient.setQueryData(key, {
                    ...oldData,
                    pages: oldData.pages.map((page) => ({
                        ...page,
                        data: page.data.filter((c) => !contact_ids.includes(c.id)),
                    })),
                });
            }

            contact_ids.forEach(id => {
                queryClient.invalidateQueries({
                    queryKey: ["contacts", id]
                });
            });
        }
    })
}
