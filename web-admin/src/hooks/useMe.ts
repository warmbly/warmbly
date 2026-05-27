import { useQuery } from "@tanstack/react-query";
import { getMe } from "@/lib/api/client/auth";

// Single source of truth for "who is the logged-in admin?". Used by
// route guards, the header user menu, and any page that wants to gate
// content on `is_admin` / specific permissions.
export function useMe() {
    return useQuery({
        queryKey: ["me"],
        queryFn: getMe,
        // The dashboard refetches on focus; the admin app is touched
        // less frequently and doesn't need that aggressiveness.
        refetchOnWindowFocus: false,
        retry: false,
        staleTime: 60_000,
    });
}
