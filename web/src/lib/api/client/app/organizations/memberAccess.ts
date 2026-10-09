import type MemberAccess from "@/lib/api/models/app/organizations/MemberAccess";
import type { SuggestedSender } from "@/lib/api/models/app/organizations/MemberAccess";
import type OrganizationMember from "@/lib/api/models/app/organizations/OrganizationMember";
import Request from "../../Request";

export async function getMemberAccess(userId: string): Promise<MemberAccess> {
    return await Request<MemberAccess>({
        method: "GET",
        url: `/organization/members/${userId}/access`,
        authorization: true,
    });
}

// Replaces the member's whole scope, so a retry lands on the same state.
export async function setMemberAccess(userId: string, access: MemberAccess): Promise<OrganizationMember> {
    return await Request<OrganizationMember>({
        method: "PUT",
        url: `/organization/members/${userId}/access`,
        data: access,
        authorization: true,
    });
}

export async function getSuggestedSenders(campaignIds: string[], folderIds: string[]): Promise<SuggestedSender[]> {
    const res = await Request<{ data: SuggestedSender[] | null }>({
        method: "GET",
        url: `/organization/access/suggested-senders`,
        params: { campaign_ids: campaignIds.join(","), campaign_folder_ids: folderIds.join(",") },
        authorization: true,
    });
    return res?.data ?? [];
}
