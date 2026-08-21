import { useQuery } from "@tanstack/react-query";
import getEmail from "@/lib/api/client/app/unibox/getEmail";

// Message bodies live in object storage and are fetched per message, so a
// thread only pays for the messages the reader actually opens. Bodies never
// change once synced, hence the long staleTime.
export default function useUniboxEmail(id: string, enabled = true) {
    return useQuery({
        queryKey: ["unibox", "email", id],
        queryFn: () => getEmail(id),
        enabled: !!id && enabled,
        staleTime: 5 * 60 * 1000,
    })
}
