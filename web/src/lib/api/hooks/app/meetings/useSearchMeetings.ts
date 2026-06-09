import { useInfiniteQuery, type InfiniteData } from "@tanstack/react-query";
import type { MeetingsPage, MeetingsSearch } from "@/lib/api/models/app/integrations/Integration";
import searchMeetings from "@/lib/api/client/app/meetings/searchMeetings";

interface UseSearchMeetingsProps {
    filters: MeetingsSearch;
    limit?: number;
    enabled?: boolean;
}

// Offset infinite scroll over booked meetings. Pages flatten into one list;
// `total` comes off the server so the UI can show "N of M".
export default function useSearchMeetings({ filters, limit = 50, enabled = true }: UseSearchMeetingsProps) {
    const queryResult = useInfiniteQuery<
        MeetingsPage,
        Error,
        InfiniteData<MeetingsPage, number>,
        [string, string, MeetingsSearch, number],
        number
    >({
        queryKey: ["meetings", "search", filters, limit],
        queryFn: async ({ pageParam }) => searchMeetings(filters, pageParam, limit),
        initialPageParam: 0,
        getNextPageParam: (lastPage) =>
            lastPage.pagination.has_more ? (lastPage.pagination.next_offset ?? undefined) : undefined,
        staleTime: 15_000,
        enabled,
    });

    const meetings = queryResult.data?.pages
        .flatMap((p) => p.data ?? [])
        .filter((m): m is NonNullable<typeof m> => m != null);

    const total = queryResult.data?.pages[0]?.pagination.total ?? 0;

    return { ...queryResult, meetings, total };
}
