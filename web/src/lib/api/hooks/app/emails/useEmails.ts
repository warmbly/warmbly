import { useInfiniteQuery, type InfiniteData } from "@tanstack/react-query";
import { DEFAULT_PAGINATION_LIMIT } from "@/lib/information";
import type GetEmails from "@/lib/api/models/app/emails/GetEmails";
import getEmails from "@/lib/api/client/app/emails/getEmails";

interface UseEmailsProps {
    query: string;
    tag: string;
    limit?: number;
    enabled?: boolean;
}

export default function useEmails({ query, tag, limit = DEFAULT_PAGINATION_LIMIT, enabled = true }: UseEmailsProps) {
    const queryResult = useInfiniteQuery<
        GetEmails,
        Error,
        InfiniteData<GetEmails, string | null>,
        [string, string, string, string, number],
        string | null
    >({
        queryKey: ["emails", "list", query, tag, limit],
        queryFn: async ({ pageParam }) => getEmails(query, pageParam, tag, limit),
        initialPageParam: null,
        getNextPageParam: (lastPage) => {
            if (lastPage.pagination.has_more) {
                return lastPage.pagination.next_cursor
            }
            return undefined
        },
        staleTime: 5 * 60 * 1000,
        gcTime: 10 * 60 * 1000,
        enabled,
    });

    const emails = queryResult.data?.pages.flatMap((p) => p.data) ?? [];

    return {
        ...queryResult,
        emails,
    };
}
