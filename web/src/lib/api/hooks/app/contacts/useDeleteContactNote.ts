import { useMutation, useQueryClient } from "@tanstack/react-query";
import deleteContactNote from "@/lib/api/client/app/contacts/deleteContactNote";

export default function useDeleteContactNote() {
    const queryClient = useQueryClient();

    return useMutation({
        mutationFn: ({ contactId, noteId }: { contactId: string; noteId: string }) =>
            deleteContactNote(contactId, noteId),
        onSuccess: (_data, variables) => {
            queryClient.invalidateQueries({
                queryKey: ["contacts", variables.contactId, "notes"],
            })
        }
    })
}
