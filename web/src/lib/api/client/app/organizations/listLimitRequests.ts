import type LimitIncreaseRequest from "@/lib/api/models/app/organizations/LimitIncreaseRequest";
import Request from "../../Request";

export default async function listLimitRequests(
    orgId: string,
): Promise<{ data: LimitIncreaseRequest[] }> {
    return await Request<{ data: LimitIncreaseRequest[] }>({
        method: "GET",
        url: `/organization/${orgId}/limit-requests`,
        authorization: true,
    });
}
