import { useQuery } from "@tanstack/react-query";
import getIncoming from "@/lib/api/client/app/unibox/getIncoming";

export default function useIncomingEmails(accountId?: string, cursor?: string) {
    return useQuery({
        queryKey: ["unibox", "incoming", accountId, cursor],
        queryFn: () => getIncoming(accountId, cursor),
    })
}
