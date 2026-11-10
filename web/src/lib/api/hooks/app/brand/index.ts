import { queryOptions, useMutation, useQuery, useQueryClient, type QueryClient } from "@tanstack/react-query";
import {
    deleteAgencyBrandImage,
    deleteWorkspaceBrandLogo,
    getAgencyBrand,
    getWorkspaceBrand,
    resetWorkspaceBrand,
    setAgencyPortalDomain,
    updateAgencyBrand,
    updateWorkspaceBrand,
    uploadAgencyBrandImage,
    uploadWorkspaceBrandLogo,
    verifyAgencyPortalDomain,
} from "@/lib/api/client/app/brand";
import type { AgencyBrandImage, AgencyBrandInput, AgencyBrandView, BrandInput, WorkspaceBrandView } from "@/lib/api/models/app/brand/Brand";

export const workspaceBrandQuery = queryOptions({
    queryKey: ["brand", "workspace"],
    queryFn: getWorkspaceBrand,
    staleTime: 60_000,
});

export const agencyBrandQuery = (agencyId: string) =>
    queryOptions({ queryKey: ["brand", "agency", agencyId], queryFn: () => getAgencyBrand(agencyId) });

export function useWorkspaceBrand(enabled = true) {
    return useQuery({ ...workspaceBrandQuery, enabled });
}

export function useAgencyBrand(agencyId: string | undefined) {
    return useQuery({ ...agencyBrandQuery(agencyId ?? ""), enabled: !!agencyId });
}

// Every write answers with the whole view, so the cache takes it as is.
function useWorkspaceWrite<T>(fn: (arg: T) => Promise<WorkspaceBrandView>) {
    const qc = useQueryClient();
    return useMutation({
        mutationFn: fn,
        onSuccess: (view) => qc.setQueryData(workspaceBrandQuery.queryKey, view),
        // Forms links and connect links are built on the portal host the brand view reports.
        onSettled: () => qc.invalidateQueries({ queryKey: ["brand"] }),
    });
}

export const useUpdateWorkspaceBrand = () => useWorkspaceWrite((input: BrandInput) => updateWorkspaceBrand(input));
export const useResetWorkspaceBrand = () => useWorkspaceWrite(() => resetWorkspaceBrand());
export const useUploadWorkspaceBrandLogo = () => useWorkspaceWrite((file: File) => uploadWorkspaceBrandLogo(file));
export const useDeleteWorkspaceBrandLogo = () => useWorkspaceWrite(() => deleteWorkspaceBrandLogo());

function settleAgency(qc: QueryClient, agencyId: string, view: AgencyBrandView) {
    qc.setQueryData(agencyBrandQuery(agencyId).queryKey, view);
}

function useAgencyWrite<T>(agencyId: string, fn: (arg: T) => Promise<AgencyBrandView>) {
    const qc = useQueryClient();
    return useMutation({
        mutationFn: fn,
        onSuccess: (view) => settleAgency(qc, agencyId, view),
        // Client workspaces inherit this brand, so their views move too.
        onSettled: () => qc.invalidateQueries({ queryKey: ["brand"] }),
    });
}

export const useUpdateAgencyBrand = (agencyId: string) =>
    useAgencyWrite(agencyId, (input: AgencyBrandInput) => updateAgencyBrand(agencyId, input));
/** Uploads the brand's logo, tab icon or sign-in image. */
export const useUploadAgencyBrandImage = (agencyId: string, image: AgencyBrandImage) =>
    useAgencyWrite(agencyId, (file: File) => uploadAgencyBrandImage(agencyId, image, file));
export const useDeleteAgencyBrandImage = (agencyId: string, image: AgencyBrandImage) =>
    useAgencyWrite(agencyId, () => deleteAgencyBrandImage(agencyId, image));
export const useSetAgencyPortalDomain = (agencyId: string) =>
    useAgencyWrite(agencyId, (domain: string) => setAgencyPortalDomain(agencyId, domain));
export const useVerifyAgencyPortalDomain = (agencyId: string) =>
    useAgencyWrite(agencyId, () => verifyAgencyPortalDomain(agencyId));

/** Where clients sign in, shown at once and put back if the server refuses. */
export function useSetAgencyPortalSignIn(agencyId: string) {
    const qc = useQueryClient();
    const key = agencyBrandQuery(agencyId).queryKey;
    return useMutation({
        mutationFn: (input: AgencyBrandInput) => updateAgencyBrand(agencyId, input),
        onMutate: async (input) => {
            await qc.cancelQueries({ queryKey: key });
            const previous = qc.getQueryData<AgencyBrandView>(key);
            if (previous && input.portal_sign_in) qc.setQueryData(key, { ...previous, portal_sign_in: input.portal_sign_in });
            return { previous };
        },
        onError: (_err, _input, ctx) => {
            if (ctx?.previous) qc.setQueryData(key, ctx.previous);
        },
        onSuccess: (view) => settleAgency(qc, agencyId, view),
        onSettled: () => qc.invalidateQueries({ queryKey: key }),
    });
}
