import { useQuery } from "@tanstack/react-query";
import getAuthCheck from "@/lib/api/client/app/emails/getAuthCheck";

// Live SPF/DKIM/DMARC auth check for a mailbox's sending domain. Off by
// default (enabled=false) so it only fires when the panel is opened — DNS
// lookups are comparatively slow and shouldn't run on every drawer mount.
export default function useAuthCheck(id: string, enabled = false) {
    return useQuery({
        queryKey: ["analytics", "accounts", id, "auth-check"],
        queryFn: () => getAuthCheck(id),
        enabled: !!id && enabled,
        staleTime: 60_000,
    });
}
