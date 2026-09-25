import type { AddContact } from "@/components/app/AddContacts";
import addContacts from "@/lib/api/client/app/contacts/addContacts";
import { useMutation, useQueryClient } from "@tanstack/react-query";

export default function useAddContacts() {
    const queryClient = useQueryClient();

    return useMutation({
        mutationFn: (contacts: AddContact[]) => addContacts(contacts),
        onSuccess: () => {
            // The campaign Leads tab is a ["contacts","list"] search scoped to one
            // campaign, so a lead created there needs the list refetched, and
            // ["campaigns"] carries the counts that just moved. A contact created
            // inside a segment is pinned into it, which moves that segment's
            // count and its pinned-contacts panel. A new custom field joins the
            // workspace's field list.
            return Promise.all([
                queryClient.invalidateQueries({ queryKey: ["contacts", "list"] }),
                queryClient.invalidateQueries({ queryKey: ["contacts", "custom-fields"] }),
                queryClient.invalidateQueries({ queryKey: ["campaigns"] }),
                queryClient.invalidateQueries({ queryKey: ["segments"] }),
            ])
        }
    })
}
