import { useQuery } from "@tanstack/react-query";
import lookupContact from "@/lib/api/client/app/contacts/lookupContact";

// Resolves a sender email to a contact (or null) for the unibox CRM panel.
export default function useContactByEmail(email: string | undefined, enabled = true) {
    return useQuery({
        queryKey: ["contacts", "by-email", email ?? ""],
        queryFn: () => lookupContact(email as string),
        enabled: enabled && !!email,
        staleTime: 60_000,
    });
}
