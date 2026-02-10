import { useMutation, useQueryClient } from "@tanstack/react-query";
import createContactNote from "@/lib/api/client/app/contacts/createContactNote";

export default function useCreateContactNote() {
    const queryClient = useQueryClient();

    return useMutation({
        mutationFn: ({ contactId, data }: { contactId: string; data: { content: string } }) =>
            createContactNote(contactId, data),
        onSuccess: (_data, variables) => {
            queryClient.invalidateQueries({
                queryKey: ["contacts", variables.contactId, "notes"],
            })
        }
    })
}
