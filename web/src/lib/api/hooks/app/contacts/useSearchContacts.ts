import { useInfiniteQuery, type InfiniteData } from "@tanstack/react-query";
import { DEFAULT_PAGINATION_LIMIT } from "@/lib/information";
import type SearchContacts from "@/lib/api/models/app/contacts/SearchContacts";
import searchContacts from "@/lib/api/client/app/contacts/searchContacts";
import type SearchContactsResult from "@/lib/api/models/app/contacts/SearchContactsResult";

interface UseSearchContactsProps {
    options: SearchContacts;
    limit?: number;
    enabled?: boolean;
}

export default function useSearchContacts({ options, limit = DEFAULT_PAGINATION_LIMIT, enabled = true }: UseSearchContactsProps) {
    const queryResult = useInfiniteQuery<
        SearchContactsResult,
        Error,
        InfiniteData<SearchContactsResult, string | null>,
        [string, string, SearchContacts, number],
        string | null
    >({
        queryKey: ["contacts", "list", options, limit],
        queryFn: async ({ pageParam }) => searchContacts(options, pageParam, limit),

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

    // Defensive: backend may serialize a nil slice as JSON null on empty
    // result sets. `flatMap((p) => p.data)` over null would yield [null].
    // Coerce + drop nulls so downstream UIs can safely read fields.
    const contacts = queryResult.data?.pages
        .flatMap((p) => p.data ?? [])
        .filter((c): c is NonNullable<typeof c> => c != null);

    return {
        ...queryResult,
        contacts,
    };
}
