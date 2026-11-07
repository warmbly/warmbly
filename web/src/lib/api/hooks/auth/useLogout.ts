import { useMutation, useQueryClient } from "@tanstack/react-query";
import logout from "../../client/auth/logout";
import { clearClientSession } from "@/lib/session";
import { allTrustedDevices, forgetAllTrustedDevices } from "@/lib/trustedDevice";

// Single source of truth for "log this user out fully". Order matters:
//
//   1. Server revoke first, so the session row is gone before we throw
//      away the access token we'd need to authenticate the revoke call.
//      Wrapped so a network error here still falls through to local
//      cleanup — a stranded server session is recoverable, a stuck
//      client is not.
//   2. clearTokens() wipes every localStorage key the auth layer uses
//      (legacy four-key set + the canonical `auth_token`). UserNav used
//      to remove a non-existent key called "token" and leak everything.
//   3. queryClient.clear() drops every cached query. Without this, the
//      next login would still see the prior user's `/auth/me`, contacts,
//      orgs, etc. because `useUser` has `refetchOnMount: false`.
//   4. persist.clearStorage() + a manual slice reset removes the
//      persisted `currentOrganization` so the next user doesn't inherit
//      it from `warmbly-storage` in localStorage.
export default function useLogout() {
    const queryClient = useQueryClient();

    return useMutation({
        mutationFn: async () => {
            try {
                await logout(allTrustedDevices());
            } catch {
                // Server revoke best-effort. We still clear locally so
                // the user isn't stuck in a "logged in" state because
                // the backend hiccuped.
            }
        },
        // Shared with the session-expiry paths, so signing out and being signed
        // out leave the browser in the same state.
        onSettled: () => {
            forgetAllTrustedDevices();
            clearClientSession(queryClient);
        },
    });
}
