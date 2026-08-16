import { useMe } from "@/hooks/useMe";
import { hasAdminPerm } from "@/lib/auth/permissions";

// True when the signed-in admin holds the bit the backend gates the matching
// route on. RequireAdmin has already resolved /auth/me by the time any page
// renders, so a missing profile here only happens mid-refresh.
export function useAdminPerm(perm: number): boolean {
    const { data: me } = useMe();
    if (!me) return true;
    return hasAdminPerm(me.admin_permissions, perm);
}
