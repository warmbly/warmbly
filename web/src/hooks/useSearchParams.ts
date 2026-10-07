// The query string as URLSearchParams; setting it navigates in place, without a scroll reset.

import * as React from "react";
import { useLocation, useRouter } from "@tanstack/react-router";
import { parseSearch } from "@/lib/routerSearch";

type SearchInit = URLSearchParams | Record<string, string>;
type SearchUpdate = SearchInit | ((prev: URLSearchParams) => SearchInit);

export function useSearchParams(): [
    URLSearchParams,
    (next: SearchUpdate, options?: { replace?: boolean }) => void,
] {
    const router = useRouter();
    const searchStr = useLocation({ select: (l) => l.searchStr });
    const params = React.useMemo(() => new URLSearchParams(searchStr), [searchStr]);

    const setParams = React.useCallback(
        (next: SearchUpdate, options?: { replace?: boolean }) => {
            // Read the live location, so two updates in one tick compose.
            const prev = new URLSearchParams(router.state.location.searchStr);
            const resolved = typeof next === "function" ? next(prev) : next;
            const str = new URLSearchParams(resolved).toString();
            void router.navigate({
                to: ".",
                search: () => parseSearch(str),
                replace: options?.replace,
                resetScroll: false,
            });
        },
        [router],
    );

    return [params, setParams];
}

export function useSearchParam(key: string): [string, (value: string) => void] {
    const [params, setParams] = useSearchParams();
    const setValue = React.useCallback((value: string) => {
        setParams((prev) => {
            const next = new URLSearchParams(prev);
            if (value) next.set(key, value);
            else next.delete(key);
            return next;
        }, { replace: true });
    }, [key, setParams]);
    return [params.get(key) ?? "", setValue];
}
