import { useQuery } from "@tanstack/react-query";
import getCustomFieldKeys from "@/lib/api/client/app/contacts/customFields";

// Distinct contact custom-field keys for the org, used to suggest real fields in
// the variable picker. Cached generously since the set changes rarely.
export default function useCustomFieldKeys() {
    return useQuery({
        queryKey: ["contacts", "custom-fields"],
        queryFn: getCustomFieldKeys,
        staleTime: 5 * 60 * 1000,
    });
}
