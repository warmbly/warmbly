import updateContactsBulk from "@/lib/api/client/app/contacts/updateContactsBulk";
import type BulkEditContacts from "@/lib/api/models/app/contacts/BulkEditContacts";
import type SearchContactsResult from "@/lib/api/models/app/contacts/SearchContactsResult";
import { useMutation, useQueryClient, type InfiniteData } from "@tanstack/react-query";
import type Contact from "@/lib/api/models/app/contacts/Contact";

export default function useUpdateContactsBulk() {
    const queryClient = useQueryClient();

    return useMutation({
        mutationFn: (options: BulkEditContacts) => updateContactsBulk(options),
        onSuccess: (data) => {
            const allLists = queryClient.getQueriesData<InfiniteData<SearchContactsResult>>({
                queryKey: ["campaigns", "list"],
            });

            for (const [key, oldData] of allLists) {
                if (!oldData) continue;

                queryClient.setQueryData(key, {
                    ...oldData,
                    pages: oldData.pages.map((page) => ({
                        ...page,
                        data: page.data.map((c) => {
                            const r = data.find(con => con.id === c.id);
                            if (!r) return c;
                            return r;
                        }),
                    })),
                });
            }

            data.forEach(c => {
                queryClient.setQueryData<Contact>(
                    ["contacts", c.id],
                    c,
                );
            })
        }
    })
}
