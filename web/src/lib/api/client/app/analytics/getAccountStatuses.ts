import type AccountStatus from "@/lib/api/models/app/analytics/AccountStatus";
import Request from "../../Request";

export default async function getAccountStatuses(): Promise<AccountStatus[]> {
    return await Request<AccountStatus[]>({
        method: "GET",
        url: `/analytics/accounts`,
        authorization: true,
    })
}
