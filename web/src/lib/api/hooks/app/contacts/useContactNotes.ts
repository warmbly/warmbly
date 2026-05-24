import { useQuery } from "@tanstack/react-query";
import listContactNotes from "@/lib/api/client/app/contacts/listContactNotes";

export default function useContactNotes(contactId: string) {
    return useQuery({
        queryKey: ["contacts", contactId, "notes"],
        queryFn: () => listContactNotes(contactId),
        enabled: !!contactId,
    })
}
