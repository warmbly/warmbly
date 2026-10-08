import { useCallback, useRef, type Dispatch, type SetStateAction } from "react";
import useBrowseState from "@/hooks/useBrowseState";
import { composeInboxParams, inboxListSchema, inboxUserOverrides, type InboxListState } from "@/lib/browse-inbox";
import type { UniboxSearchParams } from "@/lib/api/models/app/unibox/UniboxSearch";

export default function useInboxBrowseState(scope: string, base: UniboxSearchParams, accounts: { id: string; tags?: string[] }[]) {
  const [saved, setSaved] = useBrowseState<InboxListState>("unibox.list.browsing", () => ({ scope, search: "", sortBy: "newest", filters: {} }), inboxListSchema);
  const keepSearch = useRef(false);
  let current = saved;
  if (saved.scope !== scope) {
    current = { scope, search: keepSearch.current ? saved.search : "", sortBy: saved.sortBy, filters: {} };
    keepSearch.current = false;
    setSaved(current);
  }
  const baseParams = { ...base, sortBy: current.sortBy };
  const params = composeInboxParams(baseParams, current.filters, accounts);
  const setParams = useCallback<Dispatch<SetStateAction<UniboxSearchParams>>>((action) => {
    setSaved((previous) => {
      const previousBase = { ...base, sortBy: previous.sortBy };
      const previousParams = composeInboxParams(previousBase, previous.filters, accounts);
      const next = typeof action === "function" ? action(previousParams) : action;
      if (next === previousParams) return previous;
      const filters = inboxUserOverrides(next, previousBase);
      const sortBy = next.sortBy ?? "newest";
      if (sortBy === previous.sortBy && JSON.stringify(filters) === JSON.stringify(previous.filters)) return previous;
      return { ...previous, sortBy, filters };
    });
  }, [base, accounts, setSaved]);
  const setSearch = useCallback((search: string) => setSaved((previous) => previous.search === search ? previous : { ...previous, search }), [setSaved]);
  return { params, baseParams, setParams, search: current.search, setSearch, keepSearch };
}
