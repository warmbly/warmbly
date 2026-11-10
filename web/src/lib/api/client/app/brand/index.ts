import Request from "../../Request";
import type { AgencyBrandImage, AgencyBrandInput, AgencyBrandView, BrandInput, WorkspaceBrandView } from "@/lib/api/models/app/brand/Brand";

export async function getWorkspaceBrand(): Promise<WorkspaceBrandView> {
    return await Request<WorkspaceBrandView>({ method: "GET", url: "/organization/brand", authorization: true });
}

export async function updateWorkspaceBrand(input: BrandInput): Promise<WorkspaceBrandView> {
    return await Request<WorkspaceBrandView>({ method: "PUT", url: "/organization/brand", data: input, authorization: true });
}

export async function resetWorkspaceBrand(): Promise<WorkspaceBrandView> {
    return await Request<WorkspaceBrandView>({ method: "DELETE", url: "/organization/brand", authorization: true });
}

export async function uploadWorkspaceBrandLogo(file: File): Promise<WorkspaceBrandView> {
    const fd = new FormData();
    fd.append("file", file, file.name);
    return await Request<WorkspaceBrandView>({ method: "POST", url: "/organization/brand/logo", data: fd, authorization: true });
}

export async function deleteWorkspaceBrandLogo(): Promise<WorkspaceBrandView> {
    return await Request<WorkspaceBrandView>({ method: "DELETE", url: "/organization/brand/logo", authorization: true });
}

const agencyBase = (agencyId: string) => `/agency/${encodeURIComponent(agencyId)}/brand`;

export async function getAgencyBrand(agencyId: string): Promise<AgencyBrandView> {
    return await Request<AgencyBrandView>({ method: "GET", url: agencyBase(agencyId), authorization: true });
}

export async function updateAgencyBrand(agencyId: string, input: AgencyBrandInput): Promise<AgencyBrandView> {
    return await Request<AgencyBrandView>({ method: "PUT", url: agencyBase(agencyId), data: input, authorization: true });
}

/** Uploads one of the brand's images (multipart field `file`). */
export async function uploadAgencyBrandImage(agencyId: string, image: AgencyBrandImage, file: File): Promise<AgencyBrandView> {
    const fd = new FormData();
    fd.append("file", file, file.name);
    return await Request<AgencyBrandView>({ method: "POST", url: `${agencyBase(agencyId)}/${image}`, data: fd, authorization: true });
}

export async function deleteAgencyBrandImage(agencyId: string, image: AgencyBrandImage): Promise<AgencyBrandView> {
    return await Request<AgencyBrandView>({ method: "DELETE", url: `${agencyBase(agencyId)}/${image}`, authorization: true });
}

export async function setAgencyPortalDomain(agencyId: string, domain: string): Promise<AgencyBrandView> {
    return await Request<AgencyBrandView>({ method: "PUT", url: `${agencyBase(agencyId)}/domain`, data: { domain }, authorization: true });
}

export async function verifyAgencyPortalDomain(agencyId: string): Promise<AgencyBrandView> {
    return await Request<AgencyBrandView>({ method: "POST", url: `${agencyBase(agencyId)}/domain/verify`, authorization: true });
}
