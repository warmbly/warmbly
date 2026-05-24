import { useMutation, useQueryClient } from "@tanstack/react-query";
import type ContactNote from "@/lib/api/models/app/crm/ContactNote";
import updateContactNote from "@/lib/api/client/app/contacts/updateContactNote";

export default function useUpdateContactNote() {
    const queryClient = useQueryClient();

    return useMutation({
        mutationFn: ({ contactId, noteId, data }: { contactId: string; noteId: string; data: Partial<ContactNote> }) =>
            updateContactNote(contactId, noteId, data),
        onSuccess: (_data, variables) => {
            queryClient.invalidateQueries({
                queryKey: ["contacts", variables.contactId, "notes"],
            })
        }
    })
}
