import type Organization from "@/lib/api/models/app/organizations/Organization";
import Request from "../../Request";

// Backend shape: { data: [{ organization_id, role, organization: {...} }] }
// (membership rows with the org nested). Flatten into the Organization
// shape the rest of the app expects.
interface RawMembership {
    organization_id: string;
    role: string;
    permissions?: number;
    organization?: {
        id: string;
        name: string;
        slug?: string;
        avatar?: string;
        plan?: string;
        created_at: string;
    };
}

interface RawResponse {
    data: RawMembership[] | null;
}

export default async function getOrganizations(): Promise<Organization[]> {
    const res = await Request<RawResponse | Organization[]>({
        method: "GET",
        url: `/organization`,
        authorization: true,
    });
    if (Array.isArray(res)) return res;
    const rows = res?.data ?? [];
    return rows
        .filter((r) => r.organization)
        .map<Organization>((r) => ({
            id: r.organization!.id,
            name: r.organization!.name,
            avatar: r.organization!.avatar,
            plan: r.organization!.plan,
            role: r.role,
            permissions: r.permissions,
            created_at: new Date(r.organization!.created_at),
        }));
}
