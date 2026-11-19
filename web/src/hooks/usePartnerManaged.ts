import useCurrentOrganization from "@/lib/api/hooks/app/organizations/useCurrentOrganization";
import type { PartnerManaged } from "@/lib/api/models/app/organizations/Organization";

/** The reseller that manages and bills the current workspace, or null. Its members buy nothing from Warmbly directly. */
export default function usePartnerManaged(): PartnerManaged | null {
    return useCurrentOrganization().data?.partner ?? null;
}
