import type { WorkspaceCopyResponse } from "@/lib/api/models/app/workspaceCopy/WorkspaceCopy";
import Request from "../../Request";

// One key per submission, so a retried request lands on the copies it already made.
function idempotent(key: string) {
    return { "Idempotency-Key": key };
}

export async function copyCampaignToWorkspaces(
    campaignId: string,
    organizationIds: string[],
    templateIds: string[],
    key: string,
): Promise<WorkspaceCopyResponse> {
    return await Request<WorkspaceCopyResponse>({
        method: "POST",
        url: `/campaigns/${campaignId}/copy-to`,
        data: { organization_ids: organizationIds, template_ids: templateIds },
        headers: idempotent(key),
        authorization: true,
    });
}

export async function copyTemplatesToWorkspaces(
    templateIds: string[],
    organizationIds: string[],
    key: string,
): Promise<WorkspaceCopyResponse> {
    return await Request<WorkspaceCopyResponse>({
        method: "POST",
        url: "/templates/copy-to",
        data: { template_ids: templateIds, organization_ids: organizationIds },
        headers: idempotent(key),
        authorization: true,
    });
}

interface AgencyRow {
    id: string;
    name: string;
}

interface AgencyClientRow {
    organization_id: string;
}

// The agencies the caller is staff of, each with its client workspace ids.
export async function listAgencyGroups(): Promise<{ id: string; name: string; organizationIds: string[] }[]> {
    const agencies = await Request<{ data: AgencyRow[] | null }>({ method: "GET", url: "/agency", authorization: true });
    const rows = agencies.data ?? [];
    return await Promise.all(
        rows.map(async (a) => {
            const clients = await Request<{ data: AgencyClientRow[] | null }>({
                method: "GET",
                url: `/agency/${a.id}/clients`,
                authorization: true,
            });
            return { id: a.id, name: a.name, organizationIds: (clients.data ?? []).map((c) => c.organization_id) };
        }),
    );
}
