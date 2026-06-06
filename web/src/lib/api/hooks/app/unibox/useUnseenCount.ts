import { useQuery } from "@tanstack/react-query";
import getUnseenCount from "@/lib/api/client/app/unibox/getUnseenCount";

// Unread inbox count, used to seed the title + favicon badge (RealtimeManager).
//
// /unibox/count is org-scoped (resolved from the server session), so callers
// should gate on a selected workspace via `enabled` to avoid a NULL-org read
// before OrgGate has reconciled the session on a fresh login. The query key's
// root "unibox" is matched by OrgGate's post-login "sync" invalidate, so the
// count refetches against the correct session after login without a reload.
export default function useUnseenCount({ enabled = true }: { enabled?: boolean } = {}) {
    return useQuery({
        queryKey: ["unibox", "unseen-count"],
        queryFn: () => getUnseenCount(),
        staleTime: 60_000,
        enabled,
    });
}
