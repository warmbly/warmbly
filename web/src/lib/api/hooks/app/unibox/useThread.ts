import { useQuery } from "@tanstack/react-query";
import getThread from "@/lib/api/client/app/unibox/getThread";

export default function useThread(threadId: string) {
    return useQuery({
        queryKey: ["unibox", "thread", threadId],
        queryFn: () => getThread(threadId),
        enabled: !!threadId,
    })
}
