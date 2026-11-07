import { useQuery } from "@tanstack/react-query";
import listLinkedIdentities from "@/lib/api/client/auth/identities/list";

export default function useLinkedIdentities() {
    return useQuery({
        queryKey: ["auth", "identities"],
        queryFn: ({ signal }) => listLinkedIdentities(signal),
    });
}
