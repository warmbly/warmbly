import { useQuery } from "@tanstack/react-query";
import getContactDeals from "@/lib/api/client/app/contacts/getContactDeals";

export default function useContactDeals(contactId: string) {
    return useQuery({
        queryKey: ["contacts", contactId, "deals"],
        queryFn: () => getContactDeals(contactId),
        enabled: !!contactId,
    })
}
