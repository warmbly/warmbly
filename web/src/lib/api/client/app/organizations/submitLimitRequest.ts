import type LimitIncreaseRequest from "@/lib/api/models/app/organizations/LimitIncreaseRequest";
import type { CreateLimitIncreaseRequest } from "@/lib/api/models/app/organizations/LimitIncreaseRequest";
import Request from "../../Request";

export default async function submitLimitRequest(
    orgId: string,
    body: CreateLimitIncreaseRequest,
): Promise<LimitIncreaseRequest> {
    return await Request<LimitIncreaseRequest>({
        method: "POST",
        url: `/organization/${orgId}/limit-requests`,
        data: body,
        authorization: true,
    });
}
