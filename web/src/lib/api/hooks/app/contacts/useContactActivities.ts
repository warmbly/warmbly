import { useQuery } from "@tanstack/react-query";
import listContactActivities from "@/lib/api/client/app/contacts/listContactActivities";

export default function useContactActivities(contactId: string) {
    return useQuery({
        queryKey: ["contacts", contactId, "activities"],
        queryFn: () => listContactActivities(contactId),
        enabled: !!contactId,
    })
}
