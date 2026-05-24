import type AccountStatus from "@/lib/api/models/app/analytics/AccountStatus";
import Request from "../../Request";

export default async function getAccountStatus(id: string): Promise<AccountStatus> {
    return await Request<AccountStatus>({
        method: "GET",
        url: `/analytics/accounts/${id}`,
        authorization: true,
    })
}
