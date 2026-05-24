import type { AddContact } from "@/components/app/AddContacts";
import addContacts from "@/lib/api/client/app/contacts/addContacts";
import { useMutation, useQueryClient } from "@tanstack/react-query";

export default function useAddContacts() {
    const queryClient = useQueryClient();

    return useMutation({
        mutationFn: (contacts: AddContact[]) => addContacts(contacts),
        onSuccess: () => {
            queryClient.invalidateQueries({
                queryKey: ["contacts", "list"]
            })
        }
    })
}
