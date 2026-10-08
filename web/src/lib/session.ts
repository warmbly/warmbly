import type { QueryClient } from "@tanstack/react-query";

import { clearTokens } from "./auth";
import { useAppStore } from "@/stores/useAppStore";
import { resetBoot } from "./boot";

// clearClientSession drops everything the signed-in person left in this
// browser: tokens, drafts, the persisted workspace selection, and the fetched
// data in the query cache.
//
// It exists because logout did all of this and the session-expiry paths did
// only the first part, so a 401 left the previous person's workspace id, and
// their unsent reply drafts, on disk for whoever used the machine next.
//
// queryClient is optional: some callers run outside the provider.
export function clearClientSession(queryClient?: QueryClient) {
    clearTokens();
    queryClient?.clear();
    resetBoot();

    const store = useAppStore.getState();
    store.logout();
    store.setOrganizations([]);
    store.setCurrentOrganization(null);

    // Drop persisted slices (currentOrganization, theme, etc.). Theme
    // re-hydrates from the system preference on next mount, which is the right
    // default for a fresh session.
    useAppStore.persist.clearStorage();
}
