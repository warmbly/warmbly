import { useQuery } from "@tanstack/react-query";
import getThread from "@/lib/api/client/app/unibox/getThread";

export default function useThread(threadId: string | null, emailId?: string) {
    return useQuery({
        // Backend now returns the entire thread by default
        // (ThreadLimitMax = 500 in internal/app/unibox/config.go), so
        // we don't pass a limit — the server picks the right one.
        queryKey: ["unibox", "thread", threadId, emailId ?? null],
        queryFn: () => getThread(threadId!, { emailId }),
        enabled: !!threadId,
        staleTime: 30_000,
    })
}
