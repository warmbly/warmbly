import { useAccessRestricted } from "@/hooks/usePermission";
import { useAgencies } from "@/lib/api/hooks/app/agency/useAgency";
import { useWorkspaceBrand } from "@/lib/api/hooks/app/brand";
import type { PublicBrand } from "@/lib/api/models/app/brand/Brand";
import type { Organization } from "@/stores/slices/organizationSlice";
import { useAppStore } from "@/stores";
import { currentPortal } from "@/lib/portal";

/** The current workspace's agency, and whether the caller is staff of it rather than one of the client's own members. */
export function useWorkspaceAgency(): { agency?: NonNullable<Organization["agency"]>; staff: boolean } {
    const agency = useAppStore((s) => s.currentOrganization?.agency);
    const agencies = useAgencies();
    const staff = !!agency && !!agencies.data?.data.some((a) => a.id === agency.id);
    return { agency, staff };
}

// useClientBrand is the brand a client sees instead of Warmbly's: the
// workspace's own, else its agency's. Restricted members see it when one is
// set; members of an agency client who are not agency staff always see their
// agency. On an agency's portal domain everyone sees that agency. Null for everyone else.
export function useClientBrand(): PublicBrand | null {
    const restricted = useAccessRestricted();
    const { agency, staff } = useWorkspaceAgency();
    const managed = !!agency && !staff;
    const view = useWorkspaceBrand(restricted || managed);
    const effective = view.data?.effective;
    const branded = effective && effective.source !== "deployment" ? effective.brand : null;
    const portal = currentPortal();
    if (portal.portal) return portal.brand;
    if (restricted) return branded;
    if (!agency || staff) return null;
    return branded ?? { name: agency.name };
}
