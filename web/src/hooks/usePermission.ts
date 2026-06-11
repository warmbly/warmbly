import { useAppStore } from "@/stores";
import { hasPermission, PERMISSION_BITS } from "@/lib/permissions";

export type PermissionKey = keyof typeof PERMISSION_BITS;

// usePermission reports whether the current member holds a permission in the
// active workspace. The owner always passes. Returns true while permissions
// are still unknown (undefined) so pages don't flash a denial during load.
export function usePermission(key: PermissionKey): boolean {
    const org = useAppStore((s) => s.currentOrganization);
    if (!org) return true; // no org context yet — don't gate prematurely
    if (org.role === "owner") return true;
    if (org.permissions === undefined) return true; // unknown — assume yes until loaded
    return hasPermission(org.permissions, PERMISSION_BITS[key]);
}
